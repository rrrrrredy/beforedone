. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Replay"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3
$replay = [IO.File]::ReadAllText((Join-Path $env:BD_STATE "replay-path.txt")).Trim()

Write-Host "REPLAY / observable evidence only" -ForegroundColor Yellow
Show-BeforeDoneCommand "beforedone replay analyze <replay-case.json>"
& $env:BD_CLI replay analyze $replay
Start-Sleep -Seconds 3

Show-BeforeDoneCommand "beforedone replay verify <replay-case.json>"
& $env:BD_CLI replay verify $replay
Write-Host ""
Write-Host "Default behavior: DRY RUN. No imported command executed." -ForegroundColor Green
Start-Sleep -Seconds 10
