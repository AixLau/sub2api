import { promises as fs } from "node:fs";

const paths = {
  macOut: "/private/tmp/CodexUsage-Mac.command",
  winOut: "/private/tmp/CodexUsage-Windows.cmd",
  darwinArm64: "/private/tmp/codex-recover-build/codex-usage-darwin-arm64",
  darwinAmd64: "/private/tmp/codex-recover-build/codex-usage-darwin-amd64",
  windowsAmd64: "/private/tmp/codex-recover-build/codex-usage-windows-amd64.exe",
  prices: new URL("../backend/resources/model-pricing/model_prices_and_context_window.json", import.meta.url),
};

async function b64(path) {
  const raw = await fs.readFile(path);
  return raw.toString("base64").replace(/(.{1,76})/g, "$1\n");
}

async function main() {
  const mac = `#!/bin/sh
set -eu
SELF="$0"
SINCE="\${CODEX_RECOVER_SINCE:-2026-05-26}"
WORK="\${TMPDIR:-/tmp}/codex-usage-single"
mkdir -p "$WORK"
ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64) START=__DARWIN_ARM64_BEGIN__; END=__DARWIN_ARM64_END__ ;;
  x86_64|amd64) START=__DARWIN_AMD64_BEGIN__; END=__DARWIN_AMD64_END__ ;;
  *) echo "Unsupported Mac architecture: $ARCH"; echo; echo "Press Enter to exit."; read _; exit 1 ;;
esac
BIN="$WORK/codex-usage"
PRICE="$WORK/model_prices_and_context_window.json"
echo "Preparing local tool..."
awk -v start="$START" -v end="$END" '$0 == start {flag=1; next} $0 == end {flag=0} flag' "$SELF" | base64 -D > "$BIN"
awk '$0 == "__PRICE_BEGIN__" {flag=1; next} $0 == "__PRICE_END__" {flag=0} flag' "$SELF" | base64 -D > "$PRICE"
chmod +x "$BIN"
echo "Scanning local Codex sessions..."
echo
AMOUNT="$("$BIN" --since "$SINCE" --total-only --status --price-file "$PRICE")"
echo
echo "Final usage cost: \${AMOUNT} USD"
echo
echo "Press Enter to exit."
read _
exit 0
__DARWIN_ARM64_BEGIN__
${await b64(paths.darwinArm64)}__DARWIN_ARM64_END__
__DARWIN_AMD64_BEGIN__
${await b64(paths.darwinAmd64)}__DARWIN_AMD64_END__
__PRICE_BEGIN__
${await b64(paths.prices)}__PRICE_END__
`;
  await fs.writeFile(paths.macOut, mac, { mode: 0o755 });
  await fs.chmod(paths.macOut, 0o755);

  const winExe = (await b64(paths.windowsAmd64)).replace(/\n/g, "\r\n");
  const winPrices = (await b64(paths.prices)).replace(/\n/g, "\r\n");
  const win = `@echo off\r
setlocal\r
set SELF=%~f0\r
set SINCE=2026-05-26\r
if not "%CODEX_RECOVER_SINCE%"=="" set SINCE=%CODEX_RECOVER_SINCE%\r
set WORK=%TEMP%\\codex-usage-single\r
if not exist "%WORK%" mkdir "%WORK%"\r
set BIN=%WORK%\\codex-usage.exe\r
set PRICE=%WORK%\\model_prices_and_context_window.json\r
\r
echo Preparing local tool...\r
powershell -NoProfile -ExecutionPolicy Bypass -Command "$self=$env:SELF; $lines=[IO.File]::ReadAllLines($self); function Extract($a,$b,$out){ $start=[Array]::IndexOf($lines,$a); $end=[Array]::IndexOf($lines,$b); if($start -lt 0 -or $end -lt 0 -or $end -le $start){ throw 'Embedded payload missing' }; $b64=($lines[($start+1)..($end-1)] -join ''); [IO.File]::WriteAllBytes($out,[Convert]::FromBase64String($b64)) }; Extract '__WIN_EXE_BEGIN__' '__WIN_EXE_END__' $env:BIN; Extract '__PRICE_BEGIN__' '__PRICE_END__' $env:PRICE"\r
if errorlevel 1 goto failed\r
\r
echo Scanning local Codex sessions...\r
echo.\r
set RESULT=%WORK%\\amount.txt\r
"%BIN%" --since "%SINCE%" --total-only --status --price-file "%PRICE%" > "%RESULT%"\r
if errorlevel 1 goto failed\r
set /p AMOUNT=<"%RESULT%"\r
\r
echo.\r
echo Final usage cost: %AMOUNT% USD\r
echo.\r
pause\r
exit /b 0\r
\r
:failed\r
echo.\r
echo Failed to calculate usage.\r
echo.\r
pause\r
exit /b 1\r
\r
__WIN_EXE_BEGIN__\r
${winExe}__WIN_EXE_END__\r
__PRICE_BEGIN__\r
${winPrices}__PRICE_END__\r
`;
  await fs.writeFile(paths.winOut, win);

  const [macStat, winStat] = await Promise.all([fs.stat(paths.macOut), fs.stat(paths.winOut)]);
  console.log(`${paths.macOut} ${macStat.size} bytes`);
  console.log(`${paths.winOut} ${winStat.size} bytes`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
