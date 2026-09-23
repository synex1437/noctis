param(
  [Parameter(Mandatory = $true)][string]$Spec,
  [switch]$Attached
)

$launch = Get-Content -LiteralPath $Spec -Raw -Encoding UTF8 | ConvertFrom-Json
$started = [System.IO.Path]::ChangeExtension($Spec, '.started')
$pidFile = [System.IO.Path]::ChangeExtension($Spec, '.pid')
Set-Content -LiteralPath $started -Value ([string][System.Diagnostics.Process]::GetCurrentProcess().Id) -Encoding ASCII
Remove-Item Env:CLAUDECODE -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_CODE_SESSION_ID -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_CODE_CHILD_SESSION -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_CODE_SESSION_ATTENDED -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_PID -ErrorAction SilentlyContinue
Remove-Item Env:AI_AGENT -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_EFFORT -ErrorAction SilentlyContinue
Remove-Item Env:TRACEPARENT -ErrorAction SilentlyContinue
Remove-Item Env:CLAUDE_CODE_ENTRYPOINT -ErrorAction SilentlyContinue
if ($launch.configDir) { $env:CLAUDE_CONFIG_DIR = $launch.configDir } else { Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue }
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
