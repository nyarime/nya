# NYA one-click install / uninstall for Windows (PowerShell).
#
# Install (user scope, no admin):
#   irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.ps1 | iex
#
# Uninstall:
#   irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
#   # or: install.ps1 -Uninstall
#
# Installs only nya.exe (use `nya get` for downloads; no separate nya-get).
# Options: -Version 0.1.12  -Prefix DIR  -NoAssociate  -NoPath  -Uninstall

[CmdletBinding()]
param(
    [string]$Prefix = $(Join-Path $env:LOCALAPPDATA "Programs\NYA"),
    [string]$Version = "",
    [string]$Repo = "nyarime/nya",
    [switch]$Uninstall,
    [switch]$NoAssociate,
    [switch]$NoPath
)

$ErrorActionPreference = "Stop"

function Get-LatestTag {
    (Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest").tag_name
}

function Get-AssetName([string]$ver) {
    switch -Regex ($env:PROCESSOR_ARCHITECTURE) {
        "ARM64" { $goarch = "arm64" }
        "AMD64|x86_64" { $goarch = "amd64" }
        default { throw "unsupported arch: $($env:PROCESSOR_ARCHITECTURE)" }
    }
    "nya-$ver-windows-$goarch.zip"
}

function Add-UserPath([string]$dir) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $userPath) { $userPath = "" }
    $parts = @($userPath -split ";" | Where-Object { $_ -ne "" })
    if ($parts -contains $dir) { return }
    [Environment]::SetEnvironmentVariable("Path", (($parts + $dir) -join ";"), "User")
    $env:Path = "$dir;$env:Path"
    Write-Host "Added to user PATH: $dir"
}

function Remove-UserPath([string]$dir) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $userPath) { return }
    $parts = @($userPath -split ";" | Where-Object { $_ -ne "" -and $_ -ne $dir })
    [Environment]::SetEnvironmentVariable("Path", ($parts -join ";"), "User")
}

function Install-Associate([string]$nyaExe) {
    $progId = "Nyarime.NYA"
    New-Item -Path "HKCU:\Software\Classes\.nya" -Force | Out-Null
    Set-ItemProperty -Path "HKCU:\Software\Classes\.nya" -Name "(default)" -Value $progId
    New-Item -Path "HKCU:\Software\Classes\$progId" -Force | Out-Null
    Set-ItemProperty -Path "HKCU:\Software\Classes\$progId" -Name "(default)" -Value "NYA Archive"
    New-Item -Path "HKCU:\Software\Classes\$progId\shell\open\command" -Force | Out-Null
    Set-ItemProperty -Path "HKCU:\Software\Classes\$progId\shell\open\command" -Name "(default)" -Value "`"$nyaExe`" open `"%1`""
    Write-Host "Associated .nya → nya open"
}

function Uninstall-Associate {
    Remove-Item -Path "HKCU:\Software\Classes\Nyarime.NYA" -Recurse -Force -ErrorAction SilentlyContinue
    $cur = (Get-ItemProperty -Path "HKCU:\Software\Classes\.nya" -ErrorAction SilentlyContinue)."(default)"
    if ($cur -eq "Nyarime.NYA") {
        Remove-Item -Path "HKCU:\Software\Classes\.nya" -Recurse -Force -ErrorAction SilentlyContinue
    }
}

if ($Uninstall) {
    Write-Host "Uninstalling NYA from $Prefix"
    Uninstall-Associate
    Remove-UserPath $Prefix
    if (Test-Path $Prefix) { Remove-Item -Recurse -Force $Prefix }
    Write-Host "Done."
    return
}

$tag = if ($Version) { $Version } else { Get-LatestTag }
$ver = $tag.TrimStart("v")
$asset = Get-AssetName $ver
$url = "https://github.com/$Repo/releases/download/v$ver/$asset"

Write-Host "Installing NYA v$ver"
Write-Host "  from: $url"
Write-Host "  into: $Prefix"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("nya-install-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset
    Invoke-WebRequest -Uri $url -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Path $Prefix -Force | Out-Null
    $src = Join-Path $tmp "nya.exe"
    if (-not (Test-Path $src)) {
        throw "release asset missing nya.exe in $asset"
    }
    Copy-Item $src -Destination (Join-Path $Prefix "nya.exe") -Force
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$nyaExe = Join-Path $Prefix "nya.exe"
if (-not (Test-Path $nyaExe)) {
    throw "nya.exe missing after install"
}

if (-not $NoPath) { Add-UserPath $Prefix }
if (-not $NoAssociate) { Install-Associate $nyaExe }

Write-Host ""
Write-Host "Installed: $nyaExe"
Write-Host "Download: nya get --url <URL>"
Write-Host "Open a new terminal so PATH refreshes, then try: nya help"
Write-Host "Uninstall:"
Write-Host "  irm https://raw.githubusercontent.com/$Repo/main/scripts/uninstall.ps1 | iex"
if ($Prefix -ne (Join-Path $env:LOCALAPPDATA "Programs\NYA")) {
    Write-Host "  # or: uninstall.ps1 -Prefix `"$Prefix`""
}
