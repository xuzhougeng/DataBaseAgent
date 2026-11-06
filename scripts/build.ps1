# 用法：./scripts/build.ps1 [-Target windows|darwin|linux|all|setup] [-Debug]
# setup 生成 bin/DBA-Setup.exe，需要 NSIS 3（https://nsis.sourceforge.io）且 makensis 在 PATH 中；dmg 只能在 macOS 用 build.sh 生成。
# Windows 原生构建不需要 C 工具链；darwin / linux 目标用 Wails 的 purego 后端交叉编译（实验特性）。
param([string]$Target = 'windows', [switch]$Debug)
$ErrorActionPreference = 'Stop'
if (-not $env:HTTPS_PROXY) { $env:HTTP_PROXY = 'http://127.0.0.1:7897'; $env:HTTPS_PROXY = $env:HTTP_PROXY }
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
    function Build([string]$os, [string]$arch, [string]$out) {
        $tags = if ($Debug) { @() } else { @('production') }
        if ($env:TAGS) { $tags = @($env:TAGS) + $tags }
        if ($os -ne 'windows') { $tags = @('purego') + $tags }
        $ldflags = if ($Debug) { '' } else { '-s -w' }
        if ($os -eq 'windows') { $ldflags = "$ldflags -H windowsgui" }
        $env:CGO_ENABLED = '0'; $env:GOOS = $os; $env:GOARCH = $arch
        Write-Host "==> $os/$arch -> $out (tags=$($tags -join ','))"
        $args = @('build')
        if ($tags) { $args += @('-tags', ($tags -join ',')) }
        go @args -ldflags $ldflags -o $out .
        if ($LASTEXITCODE -ne 0) { throw "Go build failed: $os/$arch" }
    }
    function MacApp {
        $app = 'bin/DBA.app/Contents'
        $goVersion = go env GOVERSION
        if ($LASTEXITCODE -ne 0) { throw 'Could not determine Go version' }
        $macosTarget = if ($goVersion -match '^go1\.26(\.|$)') { '12.0' } else { '13.0' }
        New-Item -ItemType Directory -Path "$app/MacOS", "$app/Resources" -Force | Out-Null
        Build 'darwin' 'arm64' "$app/MacOS/DBA"
        Copy-Item -Path 'assets/dba-icon.icns' -Destination "$app/Resources/"
        Set-Content -NoNewline -Encoding utf8 -Path "$app/Info.plist" -Value @"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>DBA</string>
  <key>CFBundleDisplayName</key><string>DBA</string>
  <key>CFBundleExecutable</key><string>DBA</string>
  <key>CFBundleIconFile</key><string>dba-icon.icns</string>
  <key>CFBundleIdentifier</key><string>org.databaseagent.dba</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>0.1.0</string>
  <key>LSMinimumSystemVersion</key><string>$macosTarget</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
"@
    }
    switch ($Target) {
        'windows' { Build 'windows' 'amd64' 'bin/DBA.exe' }
        'linux'   { Build 'linux' 'amd64' 'bin/DBA' }
        'darwin'  { MacApp }
        'macos'   { MacApp }
        'all'     { Build 'windows' 'amd64' 'bin/DBA.exe'; Build 'linux' 'amd64' 'bin/DBA'; MacApp }
        'setup'   {
            Build 'windows' 'amd64' 'bin/DBA.exe'
            if (-not (Get-Command makensis -ErrorAction SilentlyContinue)) { throw '需要 NSIS 3（https://nsis.sourceforge.io），并把 makensis 加入 PATH' }
            Push-Location scripts
            try { makensis -V2 installer.nsi; if ($LASTEXITCODE -ne 0) { throw 'makensis failed' } } finally { Pop-Location }
            Write-Host '==> bin/DBA-Setup.exe'
        }
        'dmg'     { throw 'dmg 只能在 macOS 上用 scripts/build.sh dmg 生成' }
        default   { throw "未知目标：$Target（可选 windows、darwin、linux、all、setup）" }
    }
} finally { Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue; Pop-Location }
