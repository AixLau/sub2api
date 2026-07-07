param(
    [string]$Client = "",
    [string]$ApiKey = "",
    [switch]$ManualKey,
    [switch]$Yes
)

$ErrorActionPreference = "Stop"

$GatewayUrl = "https://aixlau.me"
$ClaudeBaseUrl = "$GatewayUrl/antigravity"
$DefaultCodexModel = "gpt-5.5"
$ManagedBegin = "# BEGIN SUB2API MANAGED BLOCK"
$ManagedEnd = "# END SUB2API MANAGED BLOCK"
$Backups = New-Object System.Collections.Generic.List[string]

if (-not ("Sub2ApiTimeoutWebClient" -as [type])) {
    Add-Type -TypeDefinition @"
using System;
using System.Net;

public class Sub2ApiTimeoutWebClient : WebClient
{
    public int TimeoutMilliseconds { get; set; }

    protected override WebRequest GetWebRequest(Uri address)
    {
        WebRequest request = base.GetWebRequest(address);
        request.Timeout = TimeoutMilliseconds;
        return request;
    }
}
"@
}

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
    $MenuTop = [Console]::CursorTop

    function Write-ClientMenu {
        param(
            [int]$SelectedIndex,
            [int]$Top
        )

        $Width = [Math]::Max(1, [Console]::BufferWidth - 1)
        $Lines = @(
            "请选择要配置的客户端（默认 Codex，↑/↓ 选择，Enter 确认）：",
            $(if ($SelectedIndex -eq 0) { "> Codex" } else { "  Codex" }),
            $(if ($SelectedIndex -eq 0) { "  Claude Code" } else { "> Claude Code" })
        )

        try {
            [Console]::SetCursorPosition(0, $Top)
            foreach ($Line in $Lines) {
                if ($Line.Length -lt $Width) {
                    [Console]::WriteLine($Line + (" " * ($Width - $Line.Length)))
                }
                else {
                    [Console]::WriteLine($Line.Substring(0, $Width))
                }
            }
            [Console]::SetCursorPosition(0, [Math]::Min($Top + $Lines.Count, [Console]::BufferHeight - 1))
        }
        catch {
            foreach ($Line in $Lines) {
                [Console]::WriteLine($Line)
            }
        }
    }

    while ($true) {
        Write-ClientMenu -SelectedIndex $Selected -Top $MenuTop

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
        }
        else {
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

function ConvertTo-TomlString {
    param([string]$Value)

    return $Value.Replace("\", "\\").Replace('"', '\"').Replace("`r", "\r").Replace("`n", "\n").Replace("`t", "\t")
}

function Test-CodexAuthInput {
    return -not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_JSON_B64) -or
        -not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_JSON) -or
        -not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_FILE)
}

function Write-CodexAuthFromInput {
    param([string]$Path)

    if (-not (Test-CodexAuthInput)) {
        return $false
    }

    $Content = ""
    if (-not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_JSON_B64)) {
        try {
            $Bytes = [Convert]::FromBase64String($env:SUB2API_CODEX_AUTH_JSON_B64)
            $Content = [Text.Encoding]::UTF8.GetString($Bytes)
        }
        catch {
            throw "SUB2API_CODEX_AUTH_JSON_B64 不是有效的 base64 内容。"
        }
    }
    elseif (-not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_JSON)) {
        $Content = $env:SUB2API_CODEX_AUTH_JSON
    }
    elseif (-not [string]::IsNullOrWhiteSpace($env:SUB2API_CODEX_AUTH_FILE)) {
        if (-not (Test-Path -LiteralPath $env:SUB2API_CODEX_AUTH_FILE -PathType Leaf)) {
            throw "Codex auth 文件不存在：$env:SUB2API_CODEX_AUTH_FILE"
        }
        $Content = Get-Content -LiteralPath $env:SUB2API_CODEX_AUTH_FILE -Raw
    }

    try {
        $null = $Content | ConvertFrom-Json
    }
    catch {
        throw "Codex auth JSON 格式无效，请检查提供的 auth 内容。"
    }

    Backup-File -Path $Path
    Set-Content -LiteralPath $Path -Value $Content -Encoding UTF8
    return $true
}

function Write-CodexApiKeyAuth {
    param([string]$Path)

    Backup-File -Path $Path
    Write-JsonObject -Path $Path -Object ([pscustomobject]@{
        OPENAI_API_KEY = $ApiKey
    })
}

function Test-CodexAuthIsOfficial {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $false
    }
    $Raw = Get-Content -LiteralPath $Path -Raw
    if ([string]::IsNullOrWhiteSpace($Raw)) {
        return $false
    }
    try {
        $Obj = $Raw | ConvertFrom-Json
    }
    catch {
        return $false
    }
    if ($Obj.auth_mode -eq "chatgpt") {
        return $true
    }
    if ($Obj.tokens -and -not [string]::IsNullOrWhiteSpace($Obj.tokens.refresh_token)) {
        return $true
    }
    return $false
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
        $Out.Add('model_reasoning_effort = "high"') | Out-Null
        $Out.Add('model_reasoning_summary = "auto"') | Out-Null
        $Out.Add('model_verbosity = "medium"') | Out-Null
        $Out.Add('disable_response_storage = true') | Out-Null
    }

    $Out.Add("") | Out-Null
    $Out.Add($ManagedBegin) | Out-Null
    $Out.Add("[model_providers.sub2api]") | Out-Null
    $Out.Add('name = "Sub2API"') | Out-Null
    $Out.Add("base_url = `"$GatewayUrl`"") | Out-Null
    $Out.Add('wire_api = "responses"') | Out-Null
    $Out.Add("requires_openai_auth = true") | Out-Null
    $Out.Add("experimental_bearer_token = `"$(ConvertTo-TomlString -Value $ApiKey)`"") | Out-Null
    $Out.Add($ManagedEnd) | Out-Null

    Set-Content -LiteralPath $Path -Value ($Out -join [Environment]::NewLine) -Encoding UTF8
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

function Invoke-JsonPost {
    param(
        [string]$Url,
        [object]$Body
    )

    $WebClient = New-Object Sub2ApiTimeoutWebClient
    try {
        $WebClient.TimeoutMilliseconds = 10000
        $WebClient.Proxy = [System.Net.WebRequest]::DefaultWebProxy
        $WebClient.Headers["Content-Type"] = "application/json"
        $Json = $Body | ConvertTo-Json -Depth 10 -Compress
        $Response = $WebClient.UploadString($Url, "POST", $Json)
        $Envelope = $Response | ConvertFrom-Json
        return $Envelope.data
    }
    finally {
        $WebClient.Dispose()
    }
}

function Invoke-JsonGet {
    param([string]$Url)

    $WebClient = New-Object Sub2ApiTimeoutWebClient
    try {
        $WebClient.TimeoutMilliseconds = 10000
        $WebClient.Proxy = [System.Net.WebRequest]::DefaultWebProxy
        $Response = $WebClient.DownloadString($Url)
        $Envelope = $Response | ConvertFrom-Json
        return $Envelope.data
    }
    finally {
        $WebClient.Dispose()
    }
}

function Get-FreeCallbackPort {
    foreach ($Port in 38173..38179) {
        $Listener = $null
        try {
            $Listener = New-Object System.Net.Sockets.TcpListener([Net.IPAddress]::Parse("127.0.0.1"), $Port)
            $Listener.Start()
            $Listener.Stop()
            return $Port
        }
        catch {
            if ($Listener) {
                $Listener.Stop()
            }
        }
    }
    return 0
}

function Try-AutoApiKey {
    if ($ManualKey -or -not [string]::IsNullOrWhiteSpace($ApiKey)) {
        return ""
    }
    if ([Console]::IsInputRedirected) {
        return ""
    }

    $Port = Get-FreeCallbackPort
    if ($Port -le 0) {
        return ""
    }

    $RedirectUri = "http://127.0.0.1:$Port/callback"
    Write-Host ""
    Write-Host "正在准备自动授权，请稍候..."
    try {
        $Session = Invoke-JsonPost -Url "$GatewayUrl/api/v1/client-setup/sessions" -Body @{
            client = $Client
            redirect_uri = $RedirectUri
        }
    }
    catch {
        return ""
    }

    if (-not $Session -or [string]::IsNullOrWhiteSpace($Session.setup_id) -or [string]::IsNullOrWhiteSpace($Session.device_code) -or [string]::IsNullOrWhiteSpace($Session.poll_token) -or [string]::IsNullOrWhiteSpace($Session.verify_url)) {
        return ""
    }

    Write-Host ""
    Write-Host "正在打开浏览器完成授权。"
    Write-Host "如果浏览器没有自动打开，请手动打开："
    Write-Host "  $($Session.verify_url)"
    Write-Host ""
    Write-Host "页面验证码："
    Write-Host "  $($Session.device_code)"

    $Listener = New-Object System.Net.HttpListener
    $Listener.Prefixes.Add("http://127.0.0.1:$Port/")
    try {
        $Listener.Start()
    }
    catch {
        return ""
    }

    try {
        Start-Process $Session.verify_url | Out-Null
    }
    catch {
        # 用户仍可手动打开链接。
    }

    $SetupToken = ""
    $Deadline = (Get-Date).AddSeconds(120)
    try {
        while ((Get-Date) -lt $Deadline) {
            if ($Listener.IsListening) {
                $Async = $Listener.BeginGetContext($null, $null)
                if ($Async.AsyncWaitHandle.WaitOne(1000)) {
                    $Context = $Listener.EndGetContext($Async)
                    $SetupToken = $Context.Request.QueryString["setup_token"]
                    $Bytes = [Text.Encoding]::UTF8.GetBytes("授权完成，可以回到终端继续。")
                    $Context.Response.ContentType = "text/plain; charset=utf-8"
                    $Context.Response.OutputStream.Write($Bytes, 0, $Bytes.Length)
                    $Context.Response.Close()
                    if (-not [string]::IsNullOrWhiteSpace($SetupToken)) {
                        break
                    }
                }
            }

            try {
                $Polled = Invoke-JsonGet -Url "$GatewayUrl/api/v1/client-setup/sessions/$($Session.setup_id)?poll_token=$([uri]::EscapeDataString($Session.poll_token))"
                if ($Polled -and -not [string]::IsNullOrWhiteSpace($Polled.setup_token)) {
                    $SetupToken = $Polled.setup_token
                    break
                }
            }
            catch {
                # 继续等待本地回调。
            }
        }
    }
    finally {
        if ($Listener.IsListening) {
            $Listener.Stop()
        }
        $Listener.Close()
    }

    if ([string]::IsNullOrWhiteSpace($SetupToken)) {
        return ""
    }

    try {
        $Exchange = Invoke-JsonPost -Url "$GatewayUrl/api/v1/client-setup/exchange" -Body @{
            setup_id = $Session.setup_id
            setup_token = $SetupToken
        }
        if ($Exchange -and -not [string]::IsNullOrWhiteSpace($Exchange.api_key)) {
            Write-Host ""
            Write-Host "授权完成，正在写入配置..."
            return $Exchange.api_key
        }
    }
    catch {
        return ""
    }

    return ""
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
    $ApiKey = Try-AutoApiKey
    if ([string]::IsNullOrWhiteSpace($ApiKey)) {
        Write-Host ""
        Write-Host "自动授权未完成，改为手动输入。"
        $SecureKey = Read-Host "请输入你的 API Key" -AsSecureString
        $Ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureKey)
        try {
            $ApiKey = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($Ptr)
        }
        finally {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($Ptr)
        }
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
$CodexAuthStatus = "missing"

if ($Client -eq "codex") {
    if (Test-CodexAuthIsOfficial -Path $CodexAuth) {
        $CodexAuthStatus = "present"
    }
    elseif (Test-CodexAuthInput) {
        $CodexAuthStatus = "will_import"
    }
    else {
        $CodexAuthStatus = "missing"
    }
}
elseif ($Client -eq "claude") {
    $null = Read-JsonObject -Path $ClaudeSettings
}

if ($Client -eq "codex") {
    $ClientLabel = "Codex"
}
else {
    $ClientLabel = "Claude Code"
}
Write-Host ""
Write-Host "正在配置 $ClientLabel，请稍候..."

if ($Client -eq "codex") {
    New-Item -ItemType Directory -Path $CodexDir -Force | Out-Null
    Backup-File -Path $CodexConfig
    Write-CodexConfig -Path $CodexConfig
    if (Test-CodexAuthIsOfficial -Path $CodexAuth) {
        $CodexAuthStatus = "present"
    }
    elseif (Write-CodexAuthFromInput -Path $CodexAuth) {
        $CodexAuthStatus = "imported"
    }
    else {
        Write-CodexApiKeyAuth -Path $CodexAuth
        $CodexAuthStatus = "api_key"
    }
}
elseif ($Client -eq "claude") {
    New-Item -ItemType Directory -Path $ClaudeDir -Force | Out-Null
    Backup-File -Path $ClaudeSettings
    Write-ClaudeSettings -Path $ClaudeSettings
}

Write-Host ""
Write-Host "配置完成。"

if ($Backups.Count -gt 0) {
    Write-Host ""
    Write-Host "备份文件："
    foreach ($Backup in $Backups) {
        Write-Host $Backup
    }
}

Write-Host ""
if ($Client -eq "codex") {
    Write-Host "请重启 Codex 后再测试。"
}
else {
    Write-Host "请重启 Claude Code 后再测试。"
}
