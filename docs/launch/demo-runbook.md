# Real demo runbook

This runbook produces a real BeforeDone core-flow recording. It is not a slide
deck: every verdict, Hook response, receipt, incident, and replay plan shown in
the final cut must come from the checked-in fixture and the actual product.

## Final-capture gate

Do not start the final recording until all of these are true:

- `https://github.com/rrrrrredy/beforedone` is public.
- The CLI installed on system `PATH` reports `beforedone 1.0.0`.
- The public Git marketplace installs the BeforeDone Plugin.
- The Plugin hooks appear in `/hooks`, have been reviewed and trusted, and a
  new Codex task can see the two bundled skills.
- The website and example report load while signed out.

The full Git Marketplace Plugin is the integration shown in this demo. The
OpenAI Public Plugins Directory package is Skills-only and cannot produce the
Stop Gate sequence. Do **not** use that Directory package as a substitute, and
do **not** also run `beforedone setup codex` or install the standalone Skills in
the capture environment. Those are alternative routes and would duplicate
hooks or workflows.

## Capture environment

- Canvas: 1920×1080 at 30 fps; export H.264 MP4 at 60–90 seconds.
- Terminal: 18–22 px monospace text, neutral dark theme, and no personal prompt,
  username, email, token, notification, or unrelated path.
- Browser: fresh profile or private window at 100% zoom.
- Audio: none. Use concise English hard captions and ship the matching `.srt`.
- Sources: actual Codex task, public Plugin, actual `beforedone` binary, actual
  Git fixture, real test output, and generated HTML report.

Record a continuous product run. Cuts may remove dead time and sensitive setup,
but must not hide the transition from a source change to stale evidence, from a
failing check to an incident, or from the fix to a fresh receipt.

## Rehearsal build from the Go module root

The final cut should use the public v1.0.0 binary. This local build exists only
for rehearsal and for validating the runbook before release. Run it exactly
from the module root so Go resolves the checked-in `go.mod`:

```powershell
$ErrorActionPreference = 'Stop'
$Repo = 'D:\Codex\beforedone'
$Go = 'D:\Codex\_tools\go1.26.5\go\bin\go.exe'
$RehearsalBin = 'D:\Codex\_tmp\beforedone-demo-rehearsal\bin\beforedone.exe'

New-Item -ItemType Directory -Force -Path (Split-Path $RehearsalBin) | Out-Null
Push-Location $Repo
try {
  & $Go build -trimpath -o $RehearsalBin .\cmd\beforedone
  if ($LASTEXITCODE -ne 0) { throw "go build exited $LASTEXITCODE" }
} finally {
  Pop-Location
}
& $RehearsalBin version
```

Never label this `dev` build as a public v1.0.0 artifact in the final video.

## Deterministic fixture preparation

Run this off camera. It verifies the intended temporary path before deletion,
copies the checked-in fixture including `.beforedone.yaml`, creates a Git
baseline, and generates the initial fresh receipt.

```powershell
$ErrorActionPreference = 'Stop'
$Repo = 'D:\Codex\beforedone'
$Demo = [IO.Path]::GetFullPath('D:\Codex\_tmp\beforedone-demo')
$ExpectedDemo = [IO.Path]::GetFullPath('D:\Codex\_tmp\beforedone-demo')
$FixtureSource = Join-Path $Repo 'fixtures\demo\stale-receipt'
$Fixture = Join-Path $Demo 'project'

if ($Demo -ne $ExpectedDemo -or -not $Demo.StartsWith('D:\Codex\_tmp\')) {
  throw "Refusing to replace unexpected demo path: $Demo"
}
if (Test-Path -LiteralPath $Demo) {
  Remove-Item -LiteralPath $Demo -Recurse -Force
}
New-Item -ItemType Directory -Path $Demo | Out-Null
Copy-Item -LiteralPath $FixtureSource -Destination $Fixture -Recurse

Push-Location $Fixture
try {
  git init -b main
  git config user.name 'BeforeDone Demo'
  git config user.email 'demo@invalid.example'
  git add .
  git commit -m 'Create deterministic BeforeDone demo fixture'

  beforedone init
  beforedone doctor
  beforedone check unit
  beforedone receipt unit
  if ($LASTEXITCODE -ne 0) { throw 'Baseline receipt is not a fresh PASS' }
} finally {
  Pop-Location
}
```

The fixture baseline must still contain:

```go
func Add(a, b int) int { return a + b }
```

and a test expecting `Add(20, 22)` to equal `42`.

## Install and trust the actual Git Marketplace Plugin

Use a clean Codex environment for the final capture:

```powershell
codex plugin marketplace add rrrrrredy/beforedone
```

Restart the ChatGPT desktop app, open the Plugins Directory in Codex, select the
`beforedone` marketplace source, open BeforeDone, and choose **Install**. Then
open `D:\Codex\_tmp\beforedone-demo\project`, run `beforedone doctor` in the
integrated terminal, open `/hooks`, inspect each BeforeDone definition, and
trust it. Start a new task after installation and trust review.

If the marketplace already exists from rehearsal, refresh it with
`codex plugin marketplace upgrade beforedone` and use the Plugins Directory to
apply the BeforeDone update. Do not record around a missing CLI, untrusted Hook,
configuration warning, or stale Plugin cache.

## Exact on-camera run

Keep the same Codex task open so its normalized events share one session ID.

### 1. Produce a genuine stale-receipt Stop

Send this exact prompt:

```text
Change Add in calculator.go so it subtracts the second operand. Do not edit the
test or proactively run a check. Report this step complete after the source
edit. If a lifecycle hook asks for specific evidence, follow that hook
instruction exactly.
```

The visible source must change from `a + b` to `a - b`. When Codex attempts to
stop, the real BeforeDone Hook must return one blocking continuation that names
the stale `unit` evidence. Retake if Codex never attempts to stop or if the Hook
response is not visible.

### 2. Run the required check and capture the incident

Do not send a second user prompt between the Hook block and its corrective
continuation. Let Codex follow the Hook's named action and run
`beforedone check unit` in the same turn. The actual Go test must fail and
BeforeDone must emit `FAIL unit (exit 1)` with a receipt path. If Codex ignores
the Hook or runs a different command, retake; do not repair the shot by pasting
output. A line containing `PASS` typed by the presenter or copied from a fixture
is not acceptable.

While the latest receipt is still FAIL, run this in the same Codex task:

```text
Run `beforedone incident --correction "The contract test still proves the old
addition behavior."` and keep every generated artifact path visible.
```

`beforedone incident` is expected to write HTML, JSON, and a Replay Case and
then return exit code `1`, because its process exit represents the captured FAIL
verdict. That exit is not a report-generation error.

### 3. Fix the contract and create fresh evidence

Send:

```text
Update calculator_test.go for the requested subtraction contract: Add(20, 22)
must equal -2. Then run `beforedone check unit` and `beforedone receipt unit`.
Show both outputs without paraphrasing them.
```

The check must emit a real PASS and `receipt unit` must show `fresh=true`. Keep
the changed source and test visible long enough to connect the receipt to the
current code.

### 4. Open the generated report

Open the exact `report.html` path printed by the earlier incident command. Show:

- the incident verdict;
- First Observable Divergence and its actual precision;
- the Claim/Evidence Matrix;
- at least two real timeline events; and
- the correction and next step.

Do not edit the generated HTML or substitute the website's illustrative report
view. The report must remain usable with the network disabled.

### 5. Replay the actual case

Return to the fixture terminal and run:

```powershell
beforedone replay analyze
beforedone replay verify
```

The first command must state that zero external commands were executed. The
second must visibly say `DRY RUN` and that commands come only from the current
`.beforedone.yaml`; it must not run `go test`. Do not use `--execute` in the
60–90 second launch video.

### 6. End on both canonical URLs

Show the public website and GitHub repository for at least three seconds:

- https://rrrrrredy.github.io/beforedone/
- https://github.com/rrrrrredy/beforedone

## 75–85 second edit map

| Time | Real product action | Required visible proof | Hard-caption draft |
| --- | --- | --- | --- |
| 00:00–00:08 | Codex changes `calculator.go` and tries to stop. | Changed operator and genuine Hook continuation. | `The code changed. The old evidence no longer applies.` |
| 00:08–00:18 | Stop Gate names `unit`. | One block, no retry loop. | `BeforeDone asks for the missing proof once.` |
| 00:18–00:30 | `beforedone check unit`. | Real Go failure, exit 1, FAIL receipt. | `Verifier output cannot be replaced by a completion claim.` |
| 00:30–00:40 | Generate the incident, then fix the test. | Real artifact paths and changed contract test. | `The failed state is captured before the fix.` |
| 00:40–00:53 | Rerun check and receipt. | PASS plus `fresh=true`. | `The new PASS is bound to the files that were checked.` |
| 00:53–01:06 | Open generated HTML. | Timeline, matrix, and First Observable Divergence. | `Observable evidence becomes a replayable incident report.` |
| 01:06–01:15 | Analyze and dry-run replay. | Zero external runs and `DRY RUN`. | `Replay analyzes first. Execution is never the default.` |
| 01:15–01:22 | Website, then GitHub. | Both canonical URLs. | `Local-only. Open source. Free.` |

## Mandatory retakes

Retake the affected segment if any of these occur:

- The Stop Gate is narration, an overlay, or pasted output rather than a live
  Plugin Hook response.
- A presenter types or echoes `FAIL`, `PASS`, `fresh=true`, or `DRY RUN` instead
  of showing CLI output.
- The report uses hand-edited JSON/HTML or the illustrative homepage component.
- The Plugin and project-local hooks are both enabled.
- A secret, username, personal email, unrelated project, or notification is
  visible.
- Captions cover a command, verdict, fingerprint, or First Observable
  Divergence.
- A cut hides the relationship between a relevant-file change and receipt
  invalidation.
- Replay verification actually executes a verifier in the launch video.

After export, verify the MP4 at 1× with sound off and at 0.75× on a 13-inch
viewport. Confirm that the hard captions match the separate `.srt`, all product
text remains legible, and the final file is 1080p, 60–90 seconds, and free of
private data.
