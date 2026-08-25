# NYA uninstall for Windows
#
#   irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
#
# Removes the user install created by install.ps1 (default:
# %LOCALAPPDATA%\Programs\NYA), clears user PATH entry, and drops .nya association.
#
# Options: -Prefix DIR  (must match install prefix)

[CmdletBinding()]
param(
    [string]$Prefix = $(Join-Path $env:LOCALAPPDATA "Programs\NYA"),
    [string]$Repo = "nyarime/nya"
)

$ErrorActionPreference = "Stop"
$local = Join-Path $PSScriptRoot "install.ps1"

Write-Host "NYA uninstall"
Write-Host "  prefix: $Prefix"

if (Test-Path $local) {
    & $local -Uninstall -Prefix $Prefix -Repo $Repo
    return
}

$remote = "https://raw.githubusercontent.com/$Repo/main/scripts/install.ps1"
& ([scriptblock]::Create((Invoke-RestMethod -Uri $remote))) -Uninstall -Prefix $Prefix -Repo $Repo
