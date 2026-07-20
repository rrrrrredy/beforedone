. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Pass"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3

Write-Host "REPAIR / new evidence from the current files" -ForegroundColor Yellow
Show-BeforeDoneCommand "# Restore calculator.go: a - b -> a + b"
$calculator = Join-Path $env:BD_DEMO "calculator.go"
$fixed = [IO.File]::ReadAllText($calculator).Replace("a - b", "a + b")
[IO.File]::WriteAllText($calculator, $fixed)

Show-BeforeDoneCommand "beforedone check unit"
& $env:BD_CLI check unit
Show-BeforeDoneCommand "beforedone receipt unit"
& $env:BD_CLI receipt unit

Write-Host ""
Write-Host "CODEX STOP HOOK / fresh PASS" -ForegroundColor Yellow
Show-BeforeDoneCommand "Codex Stop event | beforedone hook codex"
$allowed = Invoke-BeforeDonePluginHook -InputPath (Join-Path $env:BD_DEMO "stop-fresh-pass.json")
Write-Host $allowed -ForegroundColor Green
Write-Host "Empty decision object: completion is allowed." -ForegroundColor Green
Start-Sleep -Seconds 12
