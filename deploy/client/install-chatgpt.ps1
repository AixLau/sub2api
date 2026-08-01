param(
    [switch]$DryRun
)

# Standalone ChatGPT desktop installer launcher for native Windows PowerShell.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$StoreProductId = "9PLM9XGG6VKS"
$StoreUri = "ms-windows-store://pdp/?ProductId=$StoreProductId"
$OfficialDownloadPage = "https://chatgpt.com/download/"

if ($env:OS -ne "Windows_NT") {
    throw "此脚本仅适用于 Windows；macOS 请运行 install-chatgpt.sh。"
}

Write-Host "检测到 Windows。"
Write-Host "ChatGPT Microsoft Store 产品 ID：$StoreProductId"

if ($DryRun) {
    Write-Host "将打开：$StoreUri"
    Write-Host "Dry run 完成，未打开 Microsoft Store。"
    exit 0
}

try {
    Start-Process -FilePath $StoreUri | Out-Null
    Write-Host "Microsoft Store 已打开，请在商店中确认安装 ChatGPT。"
}
catch {
    Write-Warning "无法启动 Microsoft Store，将打开 OpenAI 官方下载页面。"
    try {
        Start-Process -FilePath $OfficialDownloadPage | Out-Null
    }
    catch {
        throw "无法打开安装入口。请手动访问：$OfficialDownloadPage"
    }
}
