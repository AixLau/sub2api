# Sub2API Windows PowerShell bootstrap.
# Usage: irm <url>/bootstrap.ps1 | iex

$ErrorActionPreference = "Stop"

$DefaultBaseUrl = "https://aixlau.me/install"
$BaseUrl = if ($env:SUB2API_CLIENT_SETUP_BASE_URL) { $env:SUB2API_CLIENT_SETUP_BASE_URL } else { $DefaultBaseUrl }
$BaseUrl = $BaseUrl.TrimEnd("/")
$CacheBust = [DateTime]::UtcNow.Ticks.ToString()

$LocalScript = $null
if ($PSScriptRoot) {
    $Candidate = Join-Path $PSScriptRoot "setup-sub2api-client.ps1"
    if (Test-Path -LiteralPath $Candidate) {
        $LocalScript = $Candidate
    }
}

if ($LocalScript) {
    & $LocalScript @args
    exit $LASTEXITCODE
}

$SetupUrl = "$BaseUrl/setup-sub2api-client.ps1?v=$CacheBust"
$WebClient = New-Object System.Net.WebClient
$Bytes = $WebClient.DownloadData($SetupUrl)
$Utf8NoBomStrict = New-Object System.Text.UTF8Encoding($false, $true)
$Script = $Utf8NoBomStrict.GetString($Bytes)
$TempScript = Join-Path ([System.IO.Path]::GetTempPath()) ("setup-sub2api-client-" + [System.Guid]::NewGuid().ToString("N") + ".ps1")
try {
    $Utf8Bom = New-Object System.Text.UTF8Encoding($true)
    [System.IO.File]::WriteAllText($TempScript, $Script, $Utf8Bom)
    & $TempScript @args
    exit $LASTEXITCODE
}
finally {
    if ($WebClient) {
        $WebClient.Dispose()
    }
    Remove-Item -LiteralPath $TempScript -Force -ErrorAction SilentlyContinue
}
