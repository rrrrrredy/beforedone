param(
    [Parameter(Mandatory = $true)][string] $URL,
    [Parameter(Mandatory = $true)][string] $TitleStartsWith,
    [Parameter(Mandatory = $true)][string] $Output,
    [Parameter(Mandatory = $true)][double] $DurationSeconds,
    [ValidateSet("report", "site")][string] $Sequence = "report",
    [string] $FFmpeg = "ffmpeg"
)

$ErrorActionPreference = "Stop"
$edgeCandidates = @(@(
    $env:BEFOREDONE_EDGE,
    "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    "C:\Program Files\Microsoft\Edge\Application\msedge.exe"
) | Where-Object { $_ -and (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($edgeCandidates.Count -eq 0) {
    throw "Microsoft Edge was not found. Set BEFOREDONE_EDGE to its executable."
}
$edge = $edgeCandidates[0]
$outputDirectory = Split-Path -Parent ([IO.Path]::GetFullPath($Output))
if (-not (Test-Path -LiteralPath $outputDirectory -PathType Container)) {
    throw "Output directory does not exist: $outputDirectory"
}
if (Test-Path -LiteralPath $URL -PathType Leaf) {
    $URL = [Uri]::new([IO.Path]::GetFullPath($URL)).AbsoluteUri
}

$profileRoot = if ($env:BD_STATE) { $env:BD_STATE } else { $env:TEMP }
$profile = Join-Path $profileRoot ("edge-profile-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $profile | Out-Null
$edgeArguments = @(
    "--new-window",
    "--disable-gpu",
    "--disable-software-rasterizer",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-features=msEdgeSidebarV2",
    "--user-data-dir=$profile",
    $URL
)
$launcher = Start-Process -FilePath $edge -ArgumentList $edgeArguments -PassThru

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class BeforeDoneBrowserWindow {
    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")]
    public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")]
    public static extern void mouse_event(uint flags, uint dx, uint dy, int data, UIntPtr extraInfo);
    [DllImport("user32.dll")]
    public static extern void keybd_event(byte virtualKey, byte scanCode, uint flags, UIntPtr extraInfo);
    [DllImport("user32.dll")]
    private static extern bool PostMessage(IntPtr hWnd, uint message, IntPtr wParam, IntPtr lParam);
    public static bool SendWheel(IntPtr hWnd, short delta, int x, int y) {
        int wheel = delta << 16;
        int point = (y << 16) | (x & 0xffff);
        return PostMessage(hWnd, 0x020A, new IntPtr(wheel), new IntPtr(point));
    }
}
"@ -ErrorAction SilentlyContinue

$browser = $null
$deadline = (Get-Date).AddSeconds(25)
do {
    Start-Sleep -Milliseconds 500
    $browser = Get-Process msedge -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowHandle -ne 0 -and $_.MainWindowTitle.StartsWith($TitleStartsWith, [StringComparison]::OrdinalIgnoreCase) } |
        Select-Object -First 1
} until ($browser -or (Get-Date) -gt $deadline)
if (-not $browser) {
    throw "Edge window starting with '$TitleStartsWith' did not appear."
}

[void][BeforeDoneBrowserWindow]::ShowWindow($browser.MainWindowHandle, 3)
[void][BeforeDoneBrowserWindow]::SetForegroundWindow($browser.MainWindowHandle)
# Software rendering can expose the title before the local HTML is painted.
# Wait before recording so frame zero is product UI, not a white/black preroll.
Start-Sleep -Seconds 4

# A one-second window-only warm-up forces Edge's software renderer to present
# the local page before the retained recording starts. The warm-up is deleted.
$warmup = Join-Path $profileRoot ("beforedone-edge-warmup-" + [Guid]::NewGuid().ToString("N") + ".mp4")
& $FFmpeg -y -loglevel error -f gdigrab -draw_mouse 0 -framerate 30 `
    -i "title=$($browser.MainWindowTitle)" -t 1 -c:v libx264 -preset ultrafast `
    -crf 28 -pix_fmt yuv420p -an $warmup
if ($LASTEXITCODE -ne 0) {
    throw "FFmpeg browser warm-up failed with exit $LASTEXITCODE."
}
Remove-Item -LiteralPath $warmup -Force
Start-Sleep -Seconds 1

$ffmpegInfo = [Diagnostics.ProcessStartInfo]::new()
$ffmpegInfo.FileName = $FFmpeg
$ffmpegInfo.UseShellExecute = $false
foreach ($argument in @(
    "-y", "-f", "gdigrab", "-draw_mouse", "0", "-framerate", "30",
    "-i", "title=$($browser.MainWindowTitle)", "-t", [string]$DurationSeconds,
    "-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
    "-pix_fmt", "yuv420p", "-r", "30", "-movflags", "+faststart", "-an", $Output
)) {
    [void]$ffmpegInfo.ArgumentList.Add($argument)
}
$recorder = [Diagnostics.Process]::Start($ffmpegInfo)

try {
    $keys = New-Object -ComObject WScript.Shell
    # gdigrab may need several seconds before Edge's software-rendered surface
    # is stable. The retained build trims this preroll; interactions start only
    # after the report is visibly painted.
    Start-Sleep -Seconds 6
    [void][BeforeDoneBrowserWindow]::SetForegroundWindow($browser.MainWindowHandle)
    [void]$keys.AppActivate($browser.Id)
    # Focus the document body rather than Edge's address bar before sending
    # navigation keys. Mouse movement itself is excluded from the recording.
    [void][BeforeDoneBrowserWindow]::SetCursorPos(960, 520)
    [BeforeDoneBrowserWindow]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
    [BeforeDoneBrowserWindow]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 250
    if ($Sequence -eq "site") {
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 210)
        [BeforeDoneBrowserWindow]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Milliseconds 120
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 930)
        [BeforeDoneBrowserWindow]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    }
    else {
        # Drag the visible document scrollbar. This keeps the recording as a
        # real browser interaction even when the invoking terminal cannot own
        # the foreground keyboard queue.
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 250)
        [BeforeDoneBrowserWindow]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Milliseconds 120
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 510)
        [BeforeDoneBrowserWindow]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Seconds 2
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 510)
        [BeforeDoneBrowserWindow]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Milliseconds 120
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 760)
        [BeforeDoneBrowserWindow]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Seconds 2
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 760)
        [BeforeDoneBrowserWindow]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
        Start-Sleep -Milliseconds 120
        [void][BeforeDoneBrowserWindow]::SetCursorPos(1902, 920)
        [BeforeDoneBrowserWindow]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    }
    $recorder.WaitForExit()
    if ($recorder.ExitCode -ne 0) {
        throw "FFmpeg browser capture failed with exit $($recorder.ExitCode)."
    }
}
finally {
    if (-not $recorder.HasExited) {
        Stop-Process -Id $recorder.Id -Force -ErrorAction SilentlyContinue
    }
    if ($browser -and -not $browser.HasExited) {
        Stop-Process -Id $browser.Id -Force -ErrorAction SilentlyContinue
    }
    if ($launcher -and -not $launcher.HasExited) {
        Stop-Process -Id $launcher.Id -Force -ErrorAction SilentlyContinue
    }
}
