param(
    [string] $FFmpeg = "ffmpeg",
    [string] $FFprobe = "ffprobe"
)

$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$media = Join-Path $root "media"
$raw = Join-Path $media "source\raw-clips"
$subtitle = Join-Path $media "beforedone-demo.en.srt"
$intermediate = Join-Path $media ".beforedone-demo.no-captions.mp4"
$output = Join-Path $media "beforedone-demo.mp4"

# Every input below is a retained capture of an actual visible PowerShell or
# Edge window. Do not replace these clips with rendered terminal HTML or still
# frames; the launch demo is meant to be direct product evidence.
$scenes = @(
    [ordered]@{ File = "01-core-stop-and-fail.mp4"; Start = 3.0; End = 45.0; Speed = 2.5 },
    [ordered]@{ File = "02-repair-pass-and-allow.mp4"; Start = 0.0; End = 58.0; Speed = 3.0 },
    [ordered]@{ File = "03-incident-cli.mp4"; Start = 0.0; End = 31.1; Speed = 2.2 },
    [ordered]@{ File = "04-incident-report-browser.mp4"; Start = 0.0; End = 10.0; Speed = 1.0 },
    [ordered]@{ File = "05-replay-dry-run.mp4"; Start = 0.0; End = 32.87; Speed = 2.2 },
    [ordered]@{ File = "06-site-urls-browser.mp4"; Start = 0.0; End = 8.0; Speed = 1.0 }
)

foreach ($scene in $scenes) {
    $scene.Path = Join-Path $raw $scene.File
    if (-not (Test-Path -LiteralPath $scene.Path -PathType Leaf)) {
        throw "Missing retained real-window clip: $($scene.Path)"
    }
}
if (-not (Test-Path -LiteralPath $subtitle -PathType Leaf)) {
    throw "Missing subtitle file: $subtitle"
}

$inputArguments = @("-y")
foreach ($scene in $scenes) {
    $inputArguments += @("-i", $scene.Path)
}

$filters = [Collections.Generic.List[string]]::new()
for ($index = 0; $index -lt $scenes.Count; $index++) {
    $scene = $scenes[$index]
    $trim = "trim=start=$($scene.Start):end=$($scene.End)"
    $filters.Add("[$index`:v]$trim,setpts=(PTS-STARTPTS)/$($scene.Speed),scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:black,fps=30,format=yuv420p[v$index]")
}
$concatInputs = (0..($scenes.Count - 1) | ForEach-Object { "[v$_]" }) -join ""
$filters.Add("$concatInputs" + "concat=n=$($scenes.Count):v=1:a=0[outv]")

$encodeArguments = $inputArguments + @(
    "-filter_complex", ($filters -join ";"),
    "-map", "[outv]",
    "-c:v", "libx264", "-preset", "medium", "-crf", "18",
    "-r", "30", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an",
    $intermediate
)
& $FFmpeg @encodeArguments
if ($LASTEXITCODE -ne 0) {
    throw "FFmpeg failed while composing the real-window clips."
}

$subtitlePath = $subtitle.Replace("\", "/").Replace(":", "\:").Replace("'", "\'")
$subtitleFilter = "subtitles='$subtitlePath':force_style='FontName=Arial,FontSize=12,BorderStyle=1,Outline=2,Shadow=0,Alignment=2,MarginV=34,PrimaryColour=&H00FFFFFF,OutlineColour=&HCC000000'"
& $FFmpeg -y -i $intermediate -vf $subtitleFilter -c:v libx264 -preset medium -crf 18 `
    -r 30 -pix_fmt yuv420p -movflags +faststart -an $output
if ($LASTEXITCODE -ne 0) {
    throw "FFmpeg failed while burning the English captions."
}
Remove-Item -LiteralPath $intermediate -Force

function Write-ActualCaptureStill {
    param(
        [Parameter(Mandatory = $true)][string] $Input,
        [Parameter(Mandatory = $true)][double] $At,
        [Parameter(Mandatory = $true)][string] $Destination,
        [Parameter(Mandatory = $true)][int] $Width,
        [Parameter(Mandatory = $true)][int] $Height
    )
    $filter = "scale=$Width`:$Height`:force_original_aspect_ratio=decrease,pad=$Width`:$Height`:(ow-iw)/2:(oh-ih)/2:black"
    & $FFmpeg -y -loglevel error -ss $At -i $Input -vf $filter -frames:v 1 $Destination
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to extract actual-capture still: $Destination"
    }
}

$gallery = Join-Path $media "gallery"
Write-ActualCaptureStill -Input $scenes[0].Path -At 18 -Destination (Join-Path $gallery "01-stop-hook-block.png") -Width 1270 -Height 760
Write-ActualCaptureStill -Input $scenes[1].Path -At 43 -Destination (Join-Path $gallery "02-fresh-pass-receipt.png") -Width 1270 -Height 760
Write-ActualCaptureStill -Input $scenes[3].Path -At 2 -Destination (Join-Path $gallery "03-incident-report.png") -Width 1270 -Height 760
Write-ActualCaptureStill -Input $scenes[4].Path -At 25 -Destination (Join-Path $gallery "04-replay-verify-dry-run.png") -Width 1270 -Height 760
Write-ActualCaptureStill -Input $scenes[0].Path -At 42 -Destination (Join-Path $media "youtube-cover.png") -Width 1280 -Height 720

$probe = & $FFprobe -v error -select_streams v:0 `
    -show_entries stream=codec_name,width,height,pix_fmt,r_frame_rate `
    -show_entries format=duration,size -of json $output | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) {
    throw "FFprobe failed for $output"
}
$stream = $probe.streams[0]
$duration = [double]$probe.format.duration
if ($stream.codec_name -ne "h264" -or $stream.width -ne 1920 -or $stream.height -ne 1080 -or $stream.pix_fmt -ne "yuv420p" -or $duration -lt 60 -or $duration -gt 90) {
    throw "Demo validation failed: $($stream.codec_name), $($stream.width)x$($stream.height), $($stream.pix_fmt), ${duration}s"
}

Write-Host "Built actual-window demo: $output"
Write-Host ("Duration {0:N3}s; H.264 1920x1080 yuv420p at {1}" -f $duration, $stream.r_frame_rate)
