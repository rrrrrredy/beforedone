. (Join-Path $PSScriptRoot "common.ps1")
Initialize-BeforeDoneCapture -Title "BeforeDone Capture Incident Only"
Set-Location -LiteralPath $env:BD_DEMO
Start-Sleep -Seconds 3

Write-Host "INCIDENT LAB / real artifacts from the failed run" -ForegroundColor Yellow
Show-BeforeDoneCommand "beforedone incident --correction 'Fix addition before claiming completion.'"
& $env:BD_CLI incident --correction "Fix addition before claiming completion."
$incident = Get-Content -Raw -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\latest-incident.json") | ConvertFrom-Json
Write-Host ("FOD: " + $incident.first_observable_divergence.precision + " / " + $incident.first_observable_divergence.reason) -ForegroundColor Yellow
$incidentDir = Get-ChildItem -LiteralPath (Join-Path $env:BD_DEMO ".git\beforedone\incidents") -Directory | Sort-Object LastWriteTime -Descending | Select-Object -First 1
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "report-path.txt"), (Join-Path $incidentDir.FullName "report.html"))
[IO.File]::WriteAllText((Join-Path $env:BD_STATE "replay-path.txt"), (Join-Path $incidentDir.FullName "replay-case.json"))
Write-Host "Observable evidence saved as HTML + JSON + Replay Case." -ForegroundColor Green
Start-Sleep -Seconds 12
