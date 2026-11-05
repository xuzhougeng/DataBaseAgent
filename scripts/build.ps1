param([switch]$Debug)
$ErrorActionPreference = 'Stop'
$env:HTTP_PROXY = 'http://127.0.0.1:7897'
$env:HTTPS_PROXY = 'http://127.0.0.1:7897'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
    if ($Debug) { go build -o bin/DBA-debug.exe . }
    else { go build -tags production -ldflags '-s -w -H windowsgui' -o bin/DBA.exe . }
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
} finally { Pop-Location }
