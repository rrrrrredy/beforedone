$ErrorActionPreference = "Stop"

function Initialize-BeforeDoneCapture {
    param([Parameter(Mandatory = $true)][string] $Title)

    Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class BeforeDoneConsoleWindow {
    [DllImport("kernel32.dll")]
    public static extern IntPtr GetConsoleWindow();
    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
}
"@ -ErrorAction SilentlyContinue

    [Console]::Title = $Title
    [Console]::BackgroundColor = "Black"
    [Console]::ForegroundColor = "White"
    [void][BeforeDoneConsoleWindow]::ShowWindow([BeforeDoneConsoleWindow]::GetConsoleWindow(), 3)
    Clear-Host
    Write-Host "BeforeDone / REAL POWERSHELL RUN" -ForegroundColor Cyan
    $runID = $env:BD_RUN_ID
    if ([string]::IsNullOrWhiteSpace($runID)) {
        $runID = "bd-demo-terminal"
    }
    Write-Host "Run: $runID" -ForegroundColor DarkGray
    Write-Host ""
}

function Show-BeforeDoneCommand {
    param([Parameter(Mandatory = $true)][string] $Command)
    Write-Host "PS> " -NoNewline -ForegroundColor DarkGray
    Write-Host $Command -ForegroundColor White
}

function Invoke-BeforeDonePluginHook {
    param([Parameter(Mandatory = $true)][string] $InputPath)

    $psi = New-Object Diagnostics.ProcessStartInfo
    # The plugin wrapper delegates stdin/stdout to this exact hidden CLI entry.
    # Calling it directly keeps the recorded wire exchange free of a second
    # PowerShell process's startup delay.
    $psi.FileName = $env:BD_CLI
    $psi.Arguments = "hook codex"
    $psi.WorkingDirectory = $env:BD_DEMO
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $psi
    [void]$process.Start()
    $process.StandardInput.Write([IO.File]::ReadAllText($InputPath))
    $process.StandardInput.Close()
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    if ($stderr) { Write-Host $stderr.Trim() -ForegroundColor Red }
    return $stdout.Trim()
}

function Send-BeforeDoneCodexEvent {
    param([Parameter(Mandatory = $true)][string] $Json)
    $Json | & $env:BD_CLI hook codex
    if ($LASTEXITCODE -ne 0) {
        throw "Codex event ingestion failed with exit $LASTEXITCODE"
    }
}
