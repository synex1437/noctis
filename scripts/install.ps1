param([string[]]$ConfigDir = @(), [switch]$Uninstall, [switch]$NoModel, [string]$Tool = '', [string]$Roles = '', [string]$Permissions = '', [string]$Preset = '', [string]$Updates = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$binary = Join-Path $root "bin\windows-$arch\noctis.exe"
if (-not (Test-Path $binary)) { throw "Binary not found: $binary" }
$arguments = @('install', '--source', $root)
foreach ($dir in $ConfigDir) { $arguments += @('--config-dir', $dir) }
if ($Uninstall) { $arguments += '--uninstall' }
if ($NoModel) { $arguments += '--no-model' }
if ($Tool) { $arguments += @('--host', $Tool) }
if ($Roles) { $arguments += @('--profile', $Roles) }
if ($Permissions) { $arguments += @('--permissions', $Permissions) }
if ($Preset) { $arguments += @('--preset', $Preset) }
if ($Updates) { $arguments += @('--updates', $Updates) }
& $binary @arguments
exit $LASTEXITCODE
