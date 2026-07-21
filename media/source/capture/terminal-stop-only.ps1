. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Stop"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3

Write-Host "CODEX STOP HOOK / wire invocation" -ForegroundColor Yellow
Show-BeforeDoneCommand "Codex Stop event | beforedone hook codex"
$blocked = Invoke-BeforeDonePluginHook -InputPath (Join-Path $env:BD_DEMO "stop-no-evidence.json")
Write-Host $blocked -ForegroundColor Red
Write-Host "Hook process exit: 0 / decision: BLOCK" -ForegroundColor DarkGray
Start-Sleep -Seconds 15
