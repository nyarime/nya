# NYA uninstall for Windows
#
#   irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
#
# Removes a user install created by install.ps1:
#   - binaries under -Prefix (default: %LOCALAPPDATA%\Programs\NYA)
#   - that directory from the user PATH
#   - .nya → nya open file association (if owned by NYA)
#
# Options: -Prefix DIR  (must match the install prefix)

[CmdletBinding()]
param(
    [string]$Prefix = $(Join-Path $env:LOCALAPPDATA "Programs\NYA")
)

$ErrorActionPreference = "Stop"

function Remove-UserPath([string]$dir) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $userPath) { return }
    $parts = @($userPath -split ";" | Where-Object { $_ -ne "" -and $_ -ne $dir })
    [Environment]::SetEnvironmentVariable("Path", ($parts -join ";"), "User")
}

function Uninstall-Associate {
    Remove-Item -Path "HKCU:\Software\Classes\Nyarime.NYA" -Recurse -Force -ErrorAction SilentlyContinue
    $cur = (Get-ItemProperty -Path "HKCU:\Software\Classes\.nya" -ErrorAction SilentlyContinue)."(default)"
    if ($cur -eq "Nyarime.NYA") {
        Remove-Item -Path "HKCU:\Software\Classes\.nya" -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "Uninstalling NYA"
Write-Host "  prefix: $Prefix"

Uninstall-Associate
Remove-UserPath $Prefix

if (Test-Path $Prefix) {
    Remove-Item -Recurse -Force $Prefix
    Write-Host "  removed $Prefix"
    Write-Host "Done."
} else {
    Write-Host "  nothing found under this prefix"
    Write-Host "Done."
}
