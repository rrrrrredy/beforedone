. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Core"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3

Write-Host "CODEX STOP HOOK / wire invocation" -ForegroundColor Yellow
Show-BeforeDoneCommand "Codex Stop event | beforedone hook codex"
$blocked = Invoke-BeforeDonePluginHook -InputPath (Join-Path $env:BD_DEMO "stop-no-evidence.json")
Write-Host $blocked -ForegroundColor Red
Write-Host "Hook process exit: 0" -ForegroundColor DarkGray
Start-Sleep -Seconds 4

Write-Host ""
Write-Host "REQUIRED VERIFIER / actual repository state" -ForegroundColor Yellow
Show-BeforeDoneCommand "# Agent edits calculator.go: a + b -> a - b"
Send-BeforeDoneCodexEvent -Json ('{"session_id":"capture-session","turn_id":"turn-1","cwd":"' + ($env:BD_DEMO -replace '\\','\\') + '","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"calculator.go","change":"return a - b"}}')
$calculator = Join-Path $env:BD_DEMO "calculator.go"
$broken = [IO.File]::ReadAllText($calculator).Replace("a + b", "a - b")
[IO.File]::WriteAllText($calculator, $broken)
Show-BeforeDoneCommand "git diff -- calculator.go"
& git diff -- calculator.go

Show-BeforeDoneCommand "beforedone check unit"
& $env:BD_CLI check unit
$checkExit = $LASTEXITCODE
$failedReceipt = Get-Content -Raw -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\receipts\latest-unit.json") | ConvertFrom-Json
Write-Host "Signed receipt: $($failedReceipt.verdict) / exit $($failedReceipt.exit_code)" -ForegroundColor Red
Get-Content -LiteralPath (Join-Path $env:BD_DEMO (".git\beforedone\" + $failedReceipt.log_path))

# Record the actual completed check through the public Adapter Kit. Every
# correlation field is derived from the signed receipt that the command above
# just wrote; no verdict, argv, or receipt id is hand-authored for the demo.
$receiptArgv = ConvertTo-Json -InputObject @($failedReceipt.argv) -Compress
$finishedEvent = [ordered]@{
    schema_version = 1
    id = "event-check-$($failedReceipt.id)"
    occurred_at = (Get-Date).ToUniversalTime().ToString("o")
    type = "ToolFinished"
    source = "beforedone-demo-adapter"
    session_id = "capture-session"
    turn_id = "turn-1"
    cwd = $env:BD_DEMO
    tool_name = "beforedone check"
    exit_code = [int]$failedReceipt.exit_code
    summary = "beforedone check unit wrote the signed receipt shown above"
    attributes = [ordered]@{
        receipt_id = [string]$failedReceipt.id
        check_id = [string]$failedReceipt.check_id
        verdict = [string]$failedReceipt.verdict
        argv = $receiptArgv
    }
}
Show-BeforeDoneCommand "beforedone adapter ingest -  # bind ToolFinished to the signed receipt"
$finishedEvent | ConvertTo-Json -Depth 6 -Compress | & $env:BD_CLI adapter ingest -
if ($LASTEXITCODE -ne 0) {
    throw "Receipt-bound event ingestion failed with exit $LASTEXITCODE"
}

Show-BeforeDoneCommand "beforedone incident --correction 'Fix addition before claiming completion.'"
& $env:BD_CLI incident --correction "Fix addition before claiming completion."
$incident = Get-Content -Raw -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\latest-incident.json") | ConvertFrom-Json
Write-Host ("FOD: " + $incident.first_observable_divergence.precision + " / " + $incident.first_observable_divergence.reason) -ForegroundColor Yellow
$incidentDir = Get-ChildItem -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\incidents") -Directory | Sort-Object LastWriteTime -Descending | Select-Object -First 1
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "report-path.txt"), (Join-Path $incidentDir.FullName "report.html"))
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "replay-path.txt"), (Join-Path $incidentDir.FullName "replay-case.json"))
Write-Host "Observable evidence saved as HTML + JSON + Replay Case." -ForegroundColor Green
Start-Sleep -Seconds 12
