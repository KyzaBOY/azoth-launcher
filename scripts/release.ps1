<#
.SYNOPSIS
  Builds a release of Azoth Launcher with your server address baked in.

.DESCRIPTION
  The server address is not part of the source code. This script reads it from
  release.env (gitignored; start from release.env.example):

      GATEWAY_HOST=your.server.host
      GATEWAY_PORT=6000

  runs `wails build -clean` with those values passed to the linker, and writes
  build\bin\AzothLauncher.exe.sha256 ("<hex>  AzothLauncher.exe") next to the exe.

  Works with Windows PowerShell 5.1 and PowerShell 7.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\release.ps1
#>
$ErrorActionPreference = 'Stop'

function Fail([string]$message) {
    [Console]::Error.WriteLine("release: $message")
    exit 1
}

$root = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root

# ---- release.env ------------------------------------------------------------
$envFile = Join-Path $root 'release.env'
if (-not (Test-Path -LiteralPath $envFile)) {
    Fail "release.env not found. Copy release.env.example to release.env and put your server in it."
}
$settings = @{}
foreach ($line in Get-Content -LiteralPath $envFile) {
    $text = $line.Trim()
    if ($text -eq '' -or $text.StartsWith('#')) { continue }
    $eq = $text.IndexOf('=')
    if ($eq -lt 1) { continue }
    $settings[$text.Substring(0, $eq).Trim()] = $text.Substring($eq + 1).Trim().Trim('"')
}

$gatewayHost = $settings['GATEWAY_HOST']
$gatewayPort = $settings['GATEWAY_PORT']
if (-not $gatewayHost) { Fail "GATEWAY_HOST is missing in release.env." }
if (-not $gatewayPort) { Fail "GATEWAY_PORT is missing in release.env." }
if ($gatewayHost -match '[\s:/\\"'']') { Fail "GATEWAY_HOST must be only a host name or IP, without port, scheme or spaces." }
$portNumber = 0
if (-not [int]::TryParse($gatewayPort, [ref]$portNumber) -or $portNumber -lt 1 -or $portNumber -gt 65535) {
    Fail "GATEWAY_PORT must be a number from 1 to 65535."
}

# ---- wails --------------------------------------------------------------------
$wails = Get-Command wails -ErrorAction SilentlyContinue
if ($wails) {
    $wailsExe = $wails.Source
} else {
    $wailsExe = Join-Path $env:USERPROFILE 'go\bin\wails.exe'
    if (-not (Test-Path -LiteralPath $wailsExe)) {
        Fail "wails not found. Install it with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    }
}

# ---- build --------------------------------------------------------------------
$ldflags = "-X main.gatewayHost=$gatewayHost -X main.gatewayPort=$gatewayPort"
Write-Host "Building AzothLauncher (server settings from release.env)..."
& $wailsExe build -clean -ldflags $ldflags
if ($LASTEXITCODE -ne 0) { Fail "wails build failed (exit code $LASTEXITCODE)." }

$exe = Join-Path $root 'build\bin\AzothLauncher.exe'
if (-not (Test-Path -LiteralPath $exe)) { Fail "build\bin\AzothLauncher.exe was not produced." }

# ---- checksum -------------------------------------------------------------------
$sha = [System.Security.Cryptography.SHA256]::Create()
$stream = [System.IO.File]::OpenRead($exe)
try {
    $hash = ([System.BitConverter]::ToString($sha.ComputeHash($stream)) -replace '-', '').ToLower()
} finally {
    $stream.Dispose()
    $sha.Dispose()
}
$sumFile = "$exe.sha256"
[System.IO.File]::WriteAllText($sumFile, "$hash  AzothLauncher.exe`n", [System.Text.Encoding]::ASCII)

Write-Host ""
Write-Host "Done."
Write-Host "  exe:    $exe"
Write-Host "  sha256: $hash"
