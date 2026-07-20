param(
    [Parameter(Mandatory = $true)][string] $Script,
    [Parameter(Mandatory = $true)][string] $Title,
    [Parameter(Mandatory = $true)][string] $Output,
    [Parameter(Mandatory = $true)][double] $DurationSeconds,
    [string] $FFmpeg = "ffmpeg"
)

$ErrorActionPreference = "Stop"

foreach ($name in "BD_CLI", "BD_DEMO", "BD_STATE") {
    $value = [Environment]::GetEnvironmentVariable($name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$name must be an absolute path before recording."
    }
}
if (-not [IO.Path]::IsPathRooted($env:BD_CLI) -or -not (Test-Path -LiteralPath $env:BD_CLI -PathType Leaf)) {
    throw "BD_CLI must point to the exact binary being audited."
}
if (-not (Test-Path -LiteralPath $Script -PathType Leaf)) {
    throw "Capture script does not exist: $Script"
}
$outputDirectory = Split-Path -Parent ([IO.Path]::GetFullPath($Output))
if (-not (Test-Path -LiteralPath $outputDirectory -PathType Container)) {
    throw "Output directory does not exist: $outputDirectory"
}

$process = Start-Process -FilePath "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" `
    -ArgumentList @("-NoLogo", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", $Script) `
    -WindowStyle Normal -PassThru

try {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        $process.Refresh()
    } until ($process.MainWindowTitle -eq $Title -or (Get-Date) -gt $deadline -or $process.HasExited)
    if ($process.HasExited) {
        throw "PowerShell capture process exited before recording began."
    }
    if ($process.MainWindowTitle -ne $Title) {
        throw "Window title '$Title' did not appear. Observed '$($process.MainWindowTitle)'."
    }

    & $FFmpeg -y -f gdigrab -draw_mouse 0 -framerate 30 -i "title=$Title" `
        -t $DurationSeconds -c:v libx264 -preset veryfast -crf 20 -pix_fmt yuv420p `
        -r 30 -movflags +faststart -an $Output
    if ($LASTEXITCODE -ne 0) {
        throw "FFmpeg window capture failed with exit $LASTEXITCODE."
    }
}
finally {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    }
}
