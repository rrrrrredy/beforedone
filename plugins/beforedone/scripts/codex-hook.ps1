$ErrorActionPreference = "Stop"
$env:BEFOREDONE_PLUGIN_VERSION = "1.0.0"

$beforeDone = $null
$currentDirectory = [IO.Path]::GetFullPath((Get-Location).Path).TrimEnd('\')
$worktreeRoot = $null
$cursor = [IO.DirectoryInfo](Get-Location).Path
while ($null -ne $cursor) {
    if (Test-Path -LiteralPath ([IO.Path]::Combine($cursor.FullName, ".git"))) {
        # Keep walking so a nested or attacker-created .git marker cannot make
        # a path elsewhere in the outer worktree look trusted.
        $worktreeRoot = [IO.Path]::GetFullPath($cursor.FullName).TrimEnd('\')
    }
    $cursor = $cursor.Parent
}

function Test-IsWithin([string] $Root, [string] $Path) {
    if (-not $Root) { return $false }
    $rootWithSeparator = $Root.TrimEnd('\') + '\'
    return $Path.Equals($Root, [StringComparison]::OrdinalIgnoreCase) -or
        $Path.StartsWith($rootWithSeparator, [StringComparison]::OrdinalIgnoreCase)
}

foreach ($pathEntry in ([Environment]::GetEnvironmentVariable("PATH", "Process") -split ';')) {
    $pathEntry = $pathEntry.Trim().Trim('"')
    if (-not $pathEntry -or -not [IO.Path]::IsPathRooted($pathEntry)) {
        continue
    }
    try {
        $resolvedDirectory = [IO.Path]::GetFullPath($pathEntry).TrimEnd('\')
    } catch {
        continue
    }
    if ($resolvedDirectory -ieq $currentDirectory -or (Test-IsWithin $worktreeRoot $resolvedDirectory)) {
        continue
    }
    $candidate = [IO.Path]::Combine($resolvedDirectory, "beforedone.exe")
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        try {
            $resolvedCandidate = (Resolve-Path -LiteralPath $candidate -ErrorAction Stop).ProviderPath
            $resolvedCandidate = [IO.Path]::GetFullPath($resolvedCandidate)
        } catch {
            continue
        }
        if (Test-IsWithin $worktreeRoot $resolvedCandidate) {
            continue
        }
        $beforeDone = $resolvedCandidate
        break
    }
}

if (-not $beforeDone) {
    [Console]::Out.WriteLine('{"systemMessage":"BeforeDone CLI is required by the plugin but was not found in an absolute PATH directory. Install it with: go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest - or use https://github.com/rrrrrredy/beforedone/releases/latest"}')
    exit 0
}

& $beforeDone hook codex
exit $LASTEXITCODE
