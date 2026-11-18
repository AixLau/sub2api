# Configure WorkBuddy to use the GPT-6 / 6.1 / 6.2 models of the XingLian API provider.
# Needs only Windows PowerShell 5.1+ (built into Windows 10/11); nothing to install.
#
# Usage (works from cmd.exe or PowerShell):
#   powershell -NoProfile -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm 'https://aixlau.me/workbuddy/setup.ps1'))) -Key 'sk-xxx' -Base 'https://aixlau.me/v1'"
#
# Keep this file ASCII-only: Windows PowerShell 5.1 may decode a BOM-less script
# with the ANSI code page, so Chinese text is built from code points below.
# Model rules must match frontend/src/utils/workbuddyConfig.ts.
param(
  [Parameter(Mandatory = $true)][string]$Key,
  [string]$Base = 'https://aixlau.me/v1',
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$Provider = ([string][char]0x661F) + ([string][char]0x94FE) + 'AI'   # "XingLian AI" in Chinese
$ModelPattern = '^gpt-6(?:\.[12])?(?:-|$)'
$ClientVersion = '0.158.0'
$DefaultMaxOutput = 128000
$DefaultContext = 200000

function Fail([string]$Message) {
  Write-Host "[FAILED] $Message"
  exit 1
}

$Key = $Key.Trim()
if (-not $Key) { Fail '-Key is empty.' }
$Base = $Base.Trim().TrimEnd('/')
if ($Base -notmatch '^https?://') { Fail "-Base must start with http:// or https:// ($Base)" }
if ($Base -notmatch '/v1$') { $Base = "$Base/v1" }

try {
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {}

# 1. Fetch the model catalog of the key's group.
try {
  $response = Invoke-WebRequest -UseBasicParsing -TimeoutSec 30 `
    -Uri "$Base/models?client_version=$ClientVersion" `
    -Headers @{ Authorization = "Bearer $Key"; Accept = 'application/json' }
  $stream = $response.RawContentStream
  $stream.Position = 0
  $text = (New-Object IO.StreamReader($stream, [Text.Encoding]::UTF8)).ReadToEnd()
  $catalog = $text | ConvertFrom-Json
} catch {
  $status = $null
  if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
  if ($status -eq 401 -or $status -eq 403) { Fail 'API key is invalid or not allowed. Copy the full key (starts with sk-) from the console.' }
  if ($status) { Fail "Could not load the model list: HTTP $status" }
  Fail "Could not reach ${Base}: $($_.Exception.Message)"
}
if (-not $catalog -or -not $catalog.models) { Fail 'The response is not a model catalog. Check -Base (for example https://aixlau.me/v1).' }

function PositiveInt($Value) {
  if ($Value -is [ValueType] -and [double]$Value -gt 0) { return [long][math]::Floor([double]$Value) }
  return 0
}

# 2. Convert GPT-6 series models to WorkBuddy entries.
$entries = New-Object System.Collections.ArrayList
$seen = @{}
foreach ($model in @($catalog.models)) {
  $slug = "$($model.slug)".Trim()
  if (-not $slug -or $slug -notmatch $ModelPattern -or $seen.ContainsKey($slug)) { continue }
  if ("$($model.visibility)" -eq 'hide' -or $model.supported_in_api -eq $false) { continue }
  $seen[$slug] = $true

  $context = PositiveInt $model.context_window
  if (-not $context) { $context = $DefaultContext }
  $maxContext = [math]::Max((PositiveInt $model.max_context_window), $context)
  # Assign in branches: an if-expression would unwrap a one-item array into a scalar.
  if ($maxContext -gt $context) { $lengths = @($context, $maxContext) } else { $lengths = @($context) }

  $allEfforts = New-Object System.Collections.ArrayList
  foreach ($level in @($model.supported_reasoning_levels)) {
    $effort = "$($level.effort)".Trim()
    if ($effort -and -not $allEfforts.Contains($effort)) { [void]$allEfforts.Add($effort) }
  }
  $efforts = @($allEfforts | Where-Object { $_ -ne 'none' })
  $defaultEffort = "$($model.default_reasoning_level)".Trim()
  if ($efforts -notcontains $defaultEffort) {
    $defaultEffort = if ($efforts -contains 'medium') { 'medium' } elseif ($efforts.Count) { $efforts[0] } else { '' }
  }

  $modalities = if ($null -ne $model.input_modalities) { @($model.input_modalities) } else { @('text', 'image') }
  $display = "$($model.display_name)".Trim()
  if (-not $display) { $display = $slug }

  $entry = [ordered]@{
    apiKey            = $Key
    contextWindow     = [ordered]@{ defaultLength = $maxContext; supportedLengths = $lengths }
    id                = $slug
    maxInputTokens    = $maxContext
    maxOutputTokens   = $DefaultMaxOutput
    name              = "$display " + [string][char]0x00B7 + " $Provider"
    supportsImages    = ($modalities -contains 'image')
    supportsReasoning = ($efforts.Count -gt 0)
    supportsToolCall  = $true
    url               = "$Base/chat/completions"
    vendor            = $Provider
  }
  if ($efforts.Count) {
    $entry.reasoning = [ordered]@{
      canDisableThinking = $allEfforts.Contains('none')
      defaultEffort      = $defaultEffort
      supportedEfforts   = $efforts
    }
  }
  [void]$entries.Add($entry)
}
if ($entries.Count -eq 0) { Fail 'This key''s group has no GPT-6 / 6.1 / 6.2 models. Use a key from an OpenAI group.' }

# 3. Merge into %USERPROFILE%\.workbuddy\models.json, keeping other providers' models.
$dir = Join-Path $env:USERPROFILE '.workbuddy'
$path = Join-Path $dir 'models.json'
$kept = New-Object System.Collections.ArrayList
if (Test-Path -LiteralPath $path) {
  $raw = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8).Trim([char]0xFEFF).Trim()
  if ($raw) {
    try { $existing = $raw | ConvertFrom-Json } catch { Fail "$path is not valid JSON. Stopped so your config is not overwritten; fix or rename it and retry." }
    if ($raw[0] -ne '[') { Fail "$path is not a JSON array, which WorkBuddy expects. Stopped so it is not overwritten." }
    foreach ($item in @($existing)) {
      if ($null -eq $item) { continue }
      $isOurs = ("$($item.vendor)" -eq $Provider) -or ("$($item.url)".StartsWith("$Base/"))
      if (-not $isOurs) { [void]$kept.Add($item) }
    }
  }
}

$merged = @($kept) + @($entries)
# ConvertTo-Json in 5.1 unwraps single-item arrays, so build the array text by hand.
$items = $merged | ForEach-Object { $_ | ConvertTo-Json -Depth 20 }
$json = "[`n" + ($items -join ",`n") + "`n]`n"

if ($DryRun) {
  Write-Output $json
  Write-Host "[PREVIEW] Would write $path : $($entries.Count) $Provider models, keeping $($kept.Count) other models."
  exit 0
}

New-Item -ItemType Directory -Force -Path $dir | Out-Null
$backup = ''
if (Test-Path -LiteralPath $path) {
  $stamp = Get-Date -Format 'yyyyMMddHHmmss'
  $backup = "$path.bak.$stamp"
  $n = 1
  while (Test-Path -LiteralPath $backup) { $backup = "$path.bak.$stamp-$n"; $n++ }
  Copy-Item -LiteralPath $path -Destination $backup
}
$tmp = "$path.tmp"
[IO.File]::WriteAllText($tmp, $json, (New-Object Text.UTF8Encoding($false)))   # UTF-8 without BOM
Move-Item -LiteralPath $tmp -Destination $path -Force

Write-Host "[DONE] Wrote $path"
if ($backup) { Write-Host "       Backup of the previous file: $backup" }
Write-Host "       $($entries.Count) models added: $((@($entries) | ForEach-Object { $_.id }) -join ', ')"
if ($kept.Count) { Write-Host "       Kept $($kept.Count) other models" }
Write-Host "Next: fully quit and reopen WorkBuddy, then pick a model whose name ends with $Provider."
