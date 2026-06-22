$ErrorActionPreference = "Stop"

$BaseUrl = if ($env:CODEX_RECOVER_BASE_URL) { $env:CODEX_RECOVER_BASE_URL } else { "https://ai.aixlau.me/codex-recover" }
$Since = if ($env:CODEX_RECOVER_SINCE) { $env:CODEX_RECOVER_SINCE } else { "2026-05-26" }
$TmpDir = Join-Path $env:TEMP "codex-usage-recover"
$Bin = Join-Path $TmpDir "codex-usage.exe"
$Prices = Join-Path $TmpDir "model_prices_and_context_window.json"

New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null
Write-Host "Downloading usage tool from $BaseUrl/codex-usage-windows-amd64.exe ..."
Invoke-WebRequest -UseBasicParsing "$BaseUrl/codex-usage-windows-amd64.exe" -OutFile $Bin
Write-Host "Downloading price table from $BaseUrl/model_prices_and_context_window.json ..."
Invoke-WebRequest -UseBasicParsing "$BaseUrl/model_prices_and_context_window.json" -OutFile $Prices

Write-Host "Scanning local Codex sessions..."
& $Bin --since $Since --total-only --status --price-file $Prices
