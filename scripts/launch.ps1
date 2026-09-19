param(
  [Parameter(Mandatory = $true)][string]$Spec,
  [switch]$Attached
)

$launch = Get-Content -LiteralPath $Spec -Raw -Encoding UTF8 | ConvertFrom-Json
$started = [System.IO.Path]::ChangeExtension($Spec, '.started')
$pidFile = [System.IO.Path]::ChangeExtension($Spec, '.pid')
Set-Content -LiteralPath $started -Value ([string][System.Diagnostics.Process]::GetCurrentProcess().Id) -Encoding ASCII
if ($launch.configDir) { $env:CLAUDE_CONFIG_DIR = $launch.configDir }
if ($launch.effort) { $env:CLAUDE_CODE_EFFORT_LEVEL = $launch.effort }
$env:NOCTIS_HANDOFF = $launch.sessionId
if ($launch.title) { try { $Host.UI.RawUI.WindowTitle = $launch.title } catch {} }
if ($Attached) {
  $process = Start-Process -FilePath $launch.claude -ArgumentList $launch.arguments -WorkingDirectory $launch.cwd -NoNewWindow -PassThru
} else {
  $process = Start-Process -FilePath $launch.claude -ArgumentList $launch.arguments -WorkingDirectory $launch.cwd -PassThru
}
Set-Content -LiteralPath $pidFile -Value ([string]$process.Id) -Encoding ASCII
$process.WaitForExit()
exit $process.ExitCode
