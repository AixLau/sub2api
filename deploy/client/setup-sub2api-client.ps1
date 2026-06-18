param(
    [string]$Client = "",
    [string]$ApiKey = "",
    [switch]$Yes
)

$ErrorActionPreference = "Stop"

$GatewayUrl = "https://aixlau.me"
$ClaudeBaseUrl = "$GatewayUrl/antigravity"
$DefaultCodexModel = "gpt-5.4"
$ManagedBegin = "# BEGIN SUB2API MANAGED BLOCK"
$ManagedEnd = "# END SUB2API MANAGED BLOCK"
$Backups = New-Object System.Collections.Generic.List[string]

function Normalize-Client {
    param([string]$Value)

    switch ($Value.ToLowerInvariant()) {
        "1" { return "codex" }
        "codex" { return "codex" }
        "2" { return "claude" }
        "claude" { return "claude" }
        "claude-code" { return "claude" }
        "cc" { return "claude" }
        default { return "" }
    }
}

function Choose-Client {
    if (-not [Console]::IsInputRedirected -and -not [Console]::IsOutputRedirected) {
        return Choose-ClientMenu
    }

    while ($true) {
        Write-Host "请选择要配置的客户端："
        Write-Host "  1) Codex"
        Write-Host "  2) Claude Code"
        Write-Host "默认 Codex，↑/↓ 选择，Enter 确认。"
        $Choice = Read-Host "请输入 1 或 2"
        if ([string]::IsNullOrWhiteSpace($Choice)) {
            return "codex"
        }
        $Normalized = Normalize-Client -Value $Choice
        if (-not [string]::IsNullOrWhiteSpace($Normalized)) {
            return $Normalized
        }
        Write-Host "输入无效，请输入 1 选择 Codex，或输入 2 选择 Claude Code。"
    }
}

function Choose-ClientMenu {
    $Selected = 0
    $Esc = [char]27
    while ($true) {
        [Console]::Write("`r请选择要配置的客户端（默认 Codex，↑/↓ 选择，Enter 确认）：`n")
        if ($Selected -eq 0) {
            [Console]::Write("> Codex`n  Claude Code")
        }
        else {
            [Console]::Write("  Codex`n> Claude Code")
        }

        $Key = [Console]::ReadKey($true)
        if ($Key.Key -eq [ConsoleKey]::Enter) {
            [Console]::WriteLine("")
            if ($Selected -eq 0) { return "codex" }
            return "claude"
        }
        elseif ($Key.Key -eq [ConsoleKey]::D1 -or $Key.KeyChar -eq "1") {
            [Console]::WriteLine("")
            return "codex"
        }
        elseif ($Key.Key -eq [ConsoleKey]::D2 -or $Key.KeyChar -eq "2") {
            [Console]::WriteLine("")
            return "claude"
        }
        elseif ($Key.Key -eq [ConsoleKey]::UpArrow -or $Key.Key -eq [ConsoleKey]::DownArrow) {
            if ($Selected -eq 0) { $Selected = 1 } else { $Selected = 0 }
            [Console]::Write("$Esc[2A")
        }
        else {
            [Console]::Write("$Esc[2A")
        }
    }
}

function Backup-File {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return
    }

    $Timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $Backup = "$Path.bak.$Timestamp"
    $Index = 1
    while (Test-Path -LiteralPath $Backup) {
        $Backup = "$Path.bak.$Timestamp.$Index"
        $Index++
    }
    Copy-Item -LiteralPath $Path -Destination $Backup
    $Backups.Add($Backup) | Out-Null
}

function Read-JsonObject {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return [pscustomobject]@{}
    }

    try {
        $Content = Get-Content -LiteralPath $Path -Raw
        if ([string]::IsNullOrWhiteSpace($Content)) {
            return [pscustomobject]@{}
        }
        $Object = $Content | ConvertFrom-Json
        if ($null -eq $Object) {
            return [pscustomobject]@{}
        }
        return $Object
    }
    catch {
        throw "JSON 格式无效：$Path。请修复该文件或恢复备份后重试。"
    }
}

function Set-JsonProperty {
    param(
        [object]$Object,
        [string]$Name,
        [object]$Value
    )

    if ($Object.PSObject.Properties.Name -contains $Name) {
        $Object.$Name = $Value
    }
    else {
        $Object | Add-Member -NotePropertyName $Name -NotePropertyValue $Value
    }
}

function Write-JsonObject {
    param(
        [string]$Path,
        [object]$Object
    )

    $Object | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $Path -Encoding UTF8
}

function Write-CodexConfig {
    param([string]$Path)

    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        $Content = Get-Content -LiteralPath $Path -Raw
        $Pattern = "(?s)\r?\n?# BEGIN SUB2API MANAGED BLOCK.*?# END SUB2API MANAGED BLOCK\r?\n?"
        $Content = [regex]::Replace($Content, $Pattern, "`n")
        $Lines = $Content -split "\r?\n"
        $Out = New-Object System.Collections.Generic.List[string]
        $ProviderSet = $false
        $InTable = $false

        foreach ($Line in $Lines) {
            if ($Line -match "^\[") {
                if (-not $ProviderSet) {
                    $Out.Add('model_provider = "sub2api"') | Out-Null
                    $ProviderSet = $true
                }
                $InTable = $true
            }

            if (-not $InTable -and $Line -match "^\s*model_provider\s*=") {
                if (-not $ProviderSet) {
                    $Out.Add('model_provider = "sub2api"') | Out-Null
                    $ProviderSet = $true
                }
                continue
            }
            $Out.Add($Line) | Out-Null
        }

        if (-not $ProviderSet) {
            $Out.Insert(0, 'model_provider = "sub2api"')
        }
    }
    else {
        $Out = New-Object System.Collections.Generic.List[string]
        $Out.Add('model_provider = "sub2api"') | Out-Null
        $Out.Add("model = `"$DefaultCodexModel`"") | Out-Null
    }

    $Out.Add("") | Out-Null
    $Out.Add($ManagedBegin) | Out-Null
    $Out.Add("[model_providers.sub2api]") | Out-Null
    $Out.Add('name = "Sub2API"') | Out-Null
    $Out.Add("base_url = `"$GatewayUrl`"") | Out-Null
    $Out.Add('wire_api = "responses"') | Out-Null
    $Out.Add("requires_openai_auth = true") | Out-Null
    $Out.Add($ManagedEnd) | Out-Null

    Set-Content -LiteralPath $Path -Value ($Out -join [Environment]::NewLine) -Encoding UTF8
}

function Write-CodexAuth {
    param([string]$Path)

    $Object = Read-JsonObject -Path $Path
    Set-JsonProperty -Object $Object -Name "OPENAI_API_KEY" -Value $ApiKey
    Write-JsonObject -Path $Path -Object $Object
}

function Write-ClaudeSettings {
    param([string]$Path)

    $Object = Read-JsonObject -Path $Path
    $EnvObject = $Object.env
    if ($null -eq $EnvObject) {
        $EnvObject = [pscustomobject]@{}
        Set-JsonProperty -Object $Object -Name "env" -Value $EnvObject
    }
    elseif ($EnvObject -isnot [System.Management.Automation.PSCustomObject]) {
        throw "JSON 格式无效：$Path。env 字段必须是对象。"
    }

    Set-JsonProperty -Object $EnvObject -Name "ANTHROPIC_BASE_URL" -Value $ClaudeBaseUrl
    Set-JsonProperty -Object $EnvObject -Name "ANTHROPIC_AUTH_TOKEN" -Value $ApiKey
    Write-JsonObject -Path $Path -Object $Object
}

if ([string]::IsNullOrWhiteSpace($Client)) {
    $Client = Choose-Client
}
else {
    $Client = Normalize-Client -Value $Client
    if ([string]::IsNullOrWhiteSpace($Client)) {
        throw "无效的 -Client 参数值。请使用 codex 或 claude。"
    }
}

if ([string]::IsNullOrWhiteSpace($ApiKey)) {
    $SecureKey = Read-Host "请输入你的 API Key" -AsSecureString
    $Ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureKey)
    try {
        $ApiKey = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($Ptr)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($Ptr)
    }
}

if ([string]::IsNullOrWhiteSpace($ApiKey)) {
    throw "API Key 不能为空。"
}

$CodexDir = Join-Path $HOME ".codex"
$ClaudeDir = Join-Path $HOME ".claude"
$CodexConfig = Join-Path $CodexDir "config.toml"
$CodexAuth = Join-Path $CodexDir "auth.json"
$ClaudeSettings = Join-Path $ClaudeDir "settings.json"

if ($Client -eq "codex") {
    $null = Read-JsonObject -Path $CodexAuth
}
elseif ($Client -eq "claude") {
    $null = Read-JsonObject -Path $ClaudeSettings
}

Write-Host "Sub2API 客户端配置"
Write-Host ""
Write-Host "已选择客户端："
if ($Client -eq "codex") {
    Write-Host "  Codex"
}
else {
    Write-Host "  Claude Code"
}
Write-Host ""
Write-Host "将写入以下配置文件："
if ($Client -eq "codex") {
    Write-Host "  $CodexConfig"
    Write-Host "  $CodexAuth"
}
else {
    Write-Host "  $ClaudeSettings"
}
Write-Host ""
Write-Host "接口地址："
if ($Client -eq "codex") {
    Write-Host "  $GatewayUrl"
}
else {
    Write-Host "  $ClaudeBaseUrl"
}

if ($Client -eq "codex") {
    New-Item -ItemType Directory -Path $CodexDir -Force | Out-Null
    Backup-File -Path $CodexConfig
    Backup-File -Path $CodexAuth
    Write-CodexConfig -Path $CodexConfig
    Write-CodexAuth -Path $CodexAuth
}
elseif ($Client -eq "claude") {
    New-Item -ItemType Directory -Path $ClaudeDir -Force | Out-Null
    Backup-File -Path $ClaudeSettings
    Write-ClaudeSettings -Path $ClaudeSettings
}

Write-Host ""
Write-Host "配置已写入。"
Write-Host ""
Write-Host "已配置："
if ($Client -eq "codex") {
    Write-Host "  Codex 配置：$CodexConfig"
    Write-Host "  Codex API Key：$CodexAuth"
}
else {
    Write-Host "  Claude Code 配置：$ClaudeSettings"
}
Write-Host ""
Write-Host "接口地址："
if ($Client -eq "codex") {
    Write-Host "  Codex:        $GatewayUrl"
}
else {
    Write-Host "  Claude Code:  $ClaudeBaseUrl"
}

if ($Backups.Count -gt 0) {
    Write-Host ""
    Write-Host "备份文件："
    foreach ($Backup in $Backups) {
        Write-Host $Backup
    }
}

Write-Host ""
if ($Client -eq "codex") {
    Write-Host "请重启 Codex 后再测试新配置。"
}
else {
    Write-Host "请重启 Claude Code 后再测试新配置。"
}
