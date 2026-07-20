. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Incident"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3

Write-Host "REQUIRED VERIFIER / actual failing process" -ForegroundColor Yellow
Show-BeforeDoneCommand "beforedone check unit"
& $env:BD_CLI check unit
$checkExit = $LASTEXITCODE
$failedReceipt = Get-Content -Raw -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\receipts\latest-unit.json") | ConvertFrom-Json
Write-Host "Signed receipt: $($failedReceipt.verdict) / exit $($failedReceipt.exit_code)" -ForegroundColor Red
Get-Content -LiteralPath (Join-Path $env:BD_DEMO (".git\beforedone\" + $failedReceipt.log_path))
Send-BeforeDoneCodexEvent -Json ('{"session_id":"capture-session","turn_id":"turn-1","cwd":"' + ($env:BD_DEMO -replace '\\','\\') + '","hook_event_name":"PostToolUseFailure","tool_name":"Shell","tool_response":{"exit_code":' + $checkExit + ',"stderr":"calculator_test.go: Add(20, 22) = -2, want 42"}}')

Show-BeforeDoneCommand "beforedone incident --correction 'Fix addition before claiming completion.'"
& $env:BD_CLI incident --correction "Fix addition before claiming completion."
$incident = Get-Content -Raw -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\latest-incident.json") | ConvertFrom-Json
Write-Host ("FOD: " + $incident.first_observable_divergence.precision + " / " + $incident.first_observable_divergence.reason) -ForegroundColor Yellow
$incidentDir = Get-ChildItem -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\incidents") -Directory | Sort-Object LastWriteTime -Descending | Select-Object -First 1
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "report-path.txt"), (Join-Path $incidentDir.FullName "report.html"))
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "replay-path.txt"), (Join-Path $incidentDir.FullName "replay-case.json"))
Write-Host "Observable evidence saved as HTML + JSON + Replay Case." -ForegroundColor Green
Start-Sleep -Seconds 12
