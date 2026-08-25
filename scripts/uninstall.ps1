# NYA uninstall for Windows — thin wrapper around install.ps1 -Uninstall
#
#   irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
#
# Options (same as install.ps1): -Prefix DIR

[CmdletBinding()]
param(
    [string]$Prefix = $(Join-Path $env:LOCALAPPDATA "Programs\NYA"),
    [string]$Repo = "nyarime/nya"
)

$ErrorActionPreference = "Stop"
$local = Join-Path $PSScriptRoot "install.ps1"

if (Test-Path $local) {
    & $local -Uninstall -Prefix $Prefix -Repo $Repo
    return
}

$remote = "https://raw.githubusercontent.com/$Repo/main/scripts/install.ps1"
& ([scriptblock]::Create((Invoke-RestMethod -Uri $remote))) -Uninstall -Prefix $Prefix -Repo $Repo
