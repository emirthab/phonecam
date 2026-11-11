# Builds dist\phonecam.exe (the virtual camera DLL is embedded into it).
# Requires: Go 1.22+ and MinGW-w64 (g++, windres) on PATH.
#   .\build.ps1              -> version "dev"
#   .\build.ps1 -Version 1.2.3
param([string]$Version = "dev")
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
New-Item -ItemType Directory -Force assets, dist | Out-Null

Write-Host "[1/3] virtual camera DLL"
g++ -std=c++17 -O2 -Wall -Wno-delete-non-virtual-dtor -shared -static -static-libgcc -static-libstdc++ `
    "-Wl,--no-insert-timestamp" -s `
    -o assets/phonecam_vcam.dll vcam/vcam.cpp vcam/vcam.def `
    -lmfplat -lmfuuid -lole32 -lws2_32 -luuid
if ($LASTEXITCODE -ne 0) { throw "DLL build failed" }

Write-Host "[2/3] resources (icon, manifest, version info)"
$num = "1,0,0,0"
if ($Version -match '^(\d+)\.(\d+)\.(\d+)') { $num = "$($Matches[1]),$($Matches[2]),$($Matches[3]),0" }
Set-Content -Encoding ascii assets/version.h "#define VER_NUM $num`r`n#define VER_STR `"$Version`"`r`n"
windres -O coff -o rsrc_windows_amd64.syso assets/phonecam.rc
if ($LASTEXITCODE -ne 0) { throw "windres failed" }

Write-Host "[3/3] phonecam.exe"
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$Version" -o dist/phonecam.exe .
if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
Write-Host "OK -> $PSScriptRoot\dist\phonecam.exe"
