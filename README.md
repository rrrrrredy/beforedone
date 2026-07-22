# BeforeDone

**Make coding agents prove they're done.**

BeforeDone is an open-source evidence gate and incident replay toolkit for
Codex. It turns configured checks into receipts bound to their declared
relevant-file scope, asks Codex for one corrective continuation when required
evidence is missing or stale, and reconstructs failed runs from observable
events and artifacts.

[Website and guide](https://rrrrrredy.github.io/beforedone/)

## One product, three delivery forms

- **CLI:** the source of truth for checks, receipts, incidents, replay, and
  adapter validation.
- **Codex Git Marketplace Plugin:** the automatic experience. It bundles the
  Stop Hook and both BeforeDone skills, while delegating all evidence decisions
  to the CLI.
- **Standalone Skills Pack:** the same two workflows without lifecycle hooks or
  Stop enforcement.

The CLI is required in every setup. After installing it, choose exactly one
Codex integration route:

1. the Plugin for hooks plus both bundled skills;
2. the standalone Skills Pack for a manual workflow; or
3. project-local hooks from `beforedone setup codex` for the automatic gate
   without installing the Plugin.

Do not combine these routes in one Codex environment. The Plugin plus
standalone Skills duplicates the workflows; the Plugin plus project-local
hooks runs the lifecycle integration twice.

The [OpenAI Public Plugins Directory](https://learn.chatgpt.com/docs/submit-plugins)
and the Git Marketplace are different distribution surfaces. Under the current
Directory submission categories, BeforeDone's no-MCP Directory package is
**Skills-only**. It provides the two manual workflows and cannot enforce a Stop
Hook. The full automatic gate comes from the Git Marketplace Plugin or the
project-local hooks route.

## Requirements

- A Git repository. BeforeDone resolves its local runtime through Git.
- A filesystem that supports same-directory hard links for the Git directory;
  on Windows, keep the repository on NTFS rather than FAT/exFAT removable media.
- The verifier programs named in `.beforedone.yaml`, such as `go`, `npm`, or
  `pytest`.
- Codex only if you use the Plugin, standalone Skills, or project-local hooks.

## 1. Install the CLI

With Go installed:

```sh
go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest
beforedone version
```

Alternatively, download the archive for Windows, macOS, or Linux from
[GitHub Releases](https://github.com/rrrrrredy/beforedone/releases/latest),
verify it against `checksums.txt`, and put the `beforedone` executable on
`PATH`.

To install a reproducible version with Go, replace `@latest` with a release tag,
for example `@v1.0.1`.

## 2. Initialize a repository

Run these commands from any directory inside the target Git repository:

```sh
beforedone init
beforedone doctor
```

`init` creates `.beforedone.yaml` when it is missing and initializes local
runtime data under `.git/beforedone`. It is safe to run again: an existing valid
configuration is kept.

Review the generated configuration before running checks. Commands are argv
arrays, not shell command strings:

```yaml
schema_version: 1
checks:
  test:
    argv: ["go", "test", "./..."]
    relevant_files: ["**/*.go", "go.mod", "go.sum"]
    working_directory: "."
    timeout_seconds: 600
    required: true
capture:
  max_output_bytes: 1048576
  redact_patterns:
    - '(?i)(api[_-]?key|token|password|secret)\s*[:=]\s*[^\s]+'
reports:
  retain: 20
```

### Choose credible checks

A fresh Receipt proves only that the configured verifier passed for its
declared files. It does not prove that the verifier covers every acceptance
criterion. The user does not need to diagnose the exact bug, but the task still
needs observable acceptance criteria and a credible command that tests them.

`beforedone init` is a starting point, not an automatic test designer. It
recognizes a Go module and proposes `go test ./...`; for other repositories its
`git status --short` default is only scaffolding and is not correctness proof.
Review existing test, build, lint, type-check, package-script, and CI commands,
then keep the smallest set that credibly covers the task. If coverage is
missing, add a focused regression test when that change is in scope, or report
the uncovered criterion as unverified.

### Suggested Codex prompts

Configure a repository once:

```text
Help me configure BeforeDone for this repository. Inspect the existing test,
build, lint, type-check, package-script, and CI configuration. Use
`beforedone init` only as a starting point. Configure the smallest credible set
of existing commands, include every file class that can affect each check, and
report assumptions and coverage gaps. Do not add dependencies, use
`git status --short` as proof of correctness, or invent a check merely to get
PASS.
```

Verify a task:

```text
Use BeforeDone for this task. Turn my request into observable acceptance
criteria, map them to existing tests or checks, and add the smallest regression
test when coverage is missing and that change is within scope. Before saying
done, run every required BeforeDone check and confirm fresh PASS receipts for
the current files. If any criterion lacks credible evidence, report it as
unverified instead of calling it PASS. Do not weaken checks merely to obtain
PASS.
```

These prompts help Codex propose and apply the verification contract; they do
not turn natural-language confidence into a Receipt. Review `.beforedone.yaml`
before relying on it.

Do not place credentials in verifier command-line arguments or sensitive names
in verifier paths. Evidence receipts intentionally preserve the actual argv and
working-directory metadata so a reviewer can see what ran; those structural
fields are not rewritten by output redaction. Use environment-based or native
credential mechanisms, and review receipt/report metadata before sharing it.

Run a configured check through BeforeDone, then inspect its effective result:

```sh
beforedone check test
beforedone receipt test
```

A successful process creates a `PASS` receipt for the current relevant-file
fingerprint. Changing a relevant file makes that receipt stale; changing a file
outside the check's configured patterns does not. A word such as `PASS` in
ordinary command output never overrides a non-zero process exit.

Relevant globs also include matching Git-ignored files such as generated Go
sources, and the fingerprint includes executable-mode changes. Git applies the
ignored-file pathspec before BeforeDone streams results, with hard file-count,
listing-size, and content-size limits; exceeding a limit fails closed instead
of silently omitting evidence. Submodule contents are not fingerprinted in v1:
if a `relevant_files` pattern may cover a Git submodule or a path below it, the
check fails closed instead of issuing reusable evidence.

## 3. Choose one Codex integration

### Route A: Codex Git Marketplace Plugin

The Git Marketplace Plugin includes the Stop Hook and both skills. Install the
CLI first, then add the public Git marketplace:

```sh
codex plugin marketplace add rrrrrredy/beforedone
```

Restart the ChatGPT desktop app, open the Plugins Directory in Codex, select the
`beforedone` marketplace source, open BeforeDone, and choose **Install**. Then
open `/hooks`, review and trust the BeforeDone hooks, and start a new task so the
bundled skills are available. The Plugin does not download or update the CLI
silently; a missing executable produces an actionable error.

To update this route:

```sh
go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest
codex plugin marketplace upgrade beforedone
```

Then open BeforeDone in the Codex Plugins Directory and apply the offered
update. If that surface does not offer an in-place update, uninstall and
install the Plugin again from the refreshed marketplace.

To remove this route, open BeforeDone in the Codex Plugins Directory and select
**Uninstall plugin**. If you no longer want the repository marketplace either,
remove that source separately:

```sh
codex plugin marketplace remove beforedone
```

Removing the marketplace source is not a substitute for uninstalling the
Plugin in the Plugins Directory.

### Route B: standalone Skills Pack

Choose this route instead of the Plugin. It installs the same workflows but
cannot observe lifecycle events or enforce a Stop Gate.

The Skills-only BeforeDone package submitted to the OpenAI Public Plugins
Directory belongs to this route as well. Installing that Directory package does
not install the BeforeDone Hook and does not turn the manual workflow into an
automatic gate. Install, update, or uninstall the Directory package through the
Plugins Directory UI; use the Git commands below only for the standalone pack.

Ask Codex to run the built-in skill installer once for each exact path:

```text
$skill-installer install https://github.com/rrrrrredy/beforedone/tree/main/skills/verify-before-done
```

```text
$skill-installer install https://github.com/rrrrrredy/beforedone/tree/main/skills/investigate-agent-incident
```

The skills become available on the next Codex turn. To pin them to v1.0.1,
replace `/tree/main/` with `/tree/v1.0.1/` in both URLs.

You can also install both through the third-party `skills.sh` CLI. BeforeDone
itself has no telemetry, but `skills.sh` is a separate tool and may collect its
own usage data. Disable that installer telemetry explicitly if desired:

```sh
DISABLE_TELEMETRY=1 npx skills add rrrrrredy/beforedone --skill verify-before-done --skill investigate-agent-incident --agent codex --global --yes
```

PowerShell:

```powershell
$env:DISABLE_TELEMETRY = '1'
npx skills add rrrrrredy/beforedone --skill verify-before-done --skill investigate-agent-incident --agent codex --global --yes
Remove-Item Env:DISABLE_TELEMETRY
```

To update skills installed by `$skill-installer`, remove or back up only
`verify-before-done` and `investigate-agent-incident` from `$CODEX_HOME/skills`
(by default `~/.codex/skills`), rerun the two install prompts, and start a new
turn. To uninstall this route, remove only those same two directories.

If you used `skills.sh`, keep using that installer for lifecycle management:

```sh
DISABLE_TELEMETRY=1 npx skills update --global verify-before-done investigate-agent-incident
DISABLE_TELEMETRY=1 npx skills remove --global verify-before-done investigate-agent-incident
```

### Route C: project-local Codex hooks

Choose this route instead of the Plugin and standalone Skills. It writes the
BeforeDone lifecycle handlers to the current repository's `.codex/hooks.json`
and pins them to the absolute CLI executable found during setup:

```sh
beforedone setup codex
```

Open `/hooks`, review and trust the project hooks, then start a new task. If the
CLI path changes during an upgrade, rerun `beforedone setup codex`. Remove only
the BeforeDone project hooks with:

```sh
beforedone setup codex --remove
```

## Incidents and replay

Create a self-contained HTML report, machine-readable JSON, and Replay Case
from the current repository evidence:

```sh
beforedone incident
beforedone incident --correction "The parser still mishandles escaped delimiters."
beforedone incident --transcript path/to/codex-transcript.jsonl
```

The report contains a timeline, Claim/Evidence Matrix, missing or stale
evidence, and the earliest divergence supported by the available evidence. Its
precision is exactly one of `exact_event`, `time_window`, or `unlocated`.
An exact event requires an explicit match to a verified failing Receipt; a time
window must be bounded by observed events around that check. Generic non-zero
tool exits and later user corrections do not manufacture a location. BeforeDone
does not recover hidden reasoning or chain of thought.

An optional transcript is unstable narrative context, not a trust source and
not an input to First Observable Divergence. BeforeDone accepts at most 4 MiB,
applies redaction, and stores only a bounded 16 KiB narrative excerpt plus its
SHA-256 digest and a truncation flag; it does not copy the raw transcript into
the incident.

Replay analysis never runs an external command:

```sh
beforedone replay analyze
```

Verification is also a dry run by default. It displays a plan sourced only from
the current repository configuration; argv found in an imported Replay Case is
ignored:

```sh
beforedone replay verify
beforedone replay verify --check test
```

Only an explicit `--execute` runs configured checks in a temporary detached Git
worktree:

```sh
beforedone replay verify --check test --execute
```

BeforeDone disables repository Git hooks while it creates that internal
worktree. The configured verifier still runs normally after checkout.

BeforeDone does not provide network isolation. A configured verifier can use
the network, credentials, and other resources available to that process. Replay
captures verifier output through the configured `capture.max_output_bytes`
limit before redaction and report truncation, so an unbounded verifier cannot
create an unbounded in-memory result.

## Commands and exit codes

```text
beforedone init
beforedone doctor
beforedone setup codex [--remove]
beforedone check <check-id>
beforedone receipt [check-id]
beforedone incident [--correction <text>] [--transcript <path>]
beforedone replay analyze [replay-case.json]
beforedone replay verify [replay-case.json] [--check <id>] [--execute]
beforedone adapter ingest [file|-]
beforedone adapter test [path]
beforedone licenses
```

Add `--json` to any public command for `schema_version: 1` machine output.

| Code | Meaning |
| ---: | --- |
| `0` | command succeeded or verdict is `PASS` |
| `1` | verdict is `FAIL` |
| `2` | verdict is `INCONCLUSIVE` |
| `64` | invocation or configuration error |
| `70` | internal error |

`beforedone incident` can successfully write its artifacts and still exit `1`
or `2`, because the exit code represents the incident's evidence verdict.

## Local data, privacy, and retention

`.beforedone.yaml` is repository configuration and normally belongs in version
control. Runtime artifacts live under `.git/beforedone` so they do not pollute
the working tree. They include:

- the local receipt key, receipts, check logs, and latest aliases;
- a normalized event ledger containing bounded summaries rather than a required
  raw transcript;
- incident JSON, self-contained HTML reports, Replay Cases, and—when supplied—a
  redacted bounded narrative excerpt with transcript metadata.

BeforeDone applies built-in secret patterns plus `capture.redact_patterns` and
size limits before persisting captured check output, event summaries, replay
output, user corrections, and the optional transcript excerpt. Redaction is
best effort, not a guarantee. It does not rewrite receipt argv or path metadata;
never put credentials in command-line arguments, and review artifacts before
sharing them.

`reports.retain` prunes older incident directories after a new incident is
created. In v1 it does not automatically prune receipts, logs, or the event
ledger. To erase all local BeforeDone evidence, first uninstall the selected
Codex integration, then manually remove `.git/beforedone` after reviewing the
path. Remove `.beforedone.yaml` separately only if the repository should no
longer define BeforeDone checks.

The CLI, Plugin, and Skills contain no BeforeDone telemetry, hosted API, or
cloud account. See the [privacy page](https://rrrrrredy.github.io/beforedone/privacy.html)
for the separate website and third-party-tool boundaries.

## Security and trust boundary

BeforeDone is designed to prevent non-adversarial early completion and reuse of
stale evidence. It is not a security boundary against a malicious process with
the same operating-system identity and repository write access.

Such a process can read or replace `.git/beforedone/receipt.key`, edit
`.beforedone.yaml`, alter runtime artifacts, or run a trivially passing allowed
check. It can therefore manufacture a self-consistent `PASS`. Receipt signing
detects corruption and inconsistent artifacts within the supported workflow;
it is not remote attestation and does not make an untrusted same-user Agent
honest.

Treat BeforeDone as an inspectable process guardrail. Use OS isolation,
least-privilege credentials, protected configuration, or an external verifier
when the Agent itself is inside the threat model. Read [SECURITY.md](SECURITY.md)
before relying on receipts in a hostile environment.

## Adapters

The v1 normalized event contract covers `SessionStarted`, `PromptSubmitted`,
`ToolStarted`, `ToolFinished`, `AgentStopping`, and `SessionEnded`. Codex is the
only officially supported v1 adapter. The public schemas, fixtures, and
`beforedone adapter test` command form an Adapter Kit for future integrations;
their presence is not a compatibility promise for other agents. A normalized
event is limited to 256 attributes and 1 MiB after JSON encoding; the local
event ledger is read fail-closed once it exceeds 64 MiB. Review and rotate the
ledger before that boundary if a long-running repository produces many events.
The v1 writer revalidates committed segments and ID claims before every append;
this favors integrity over constant-time writes, so rotate earlier if hook
latency starts approaching the configured timeout on a very large ledger.
Event IDs must be unique within the ledger. BeforeDone stores writer batches as
immutable, content-addressed segments under `.git/beforedone/events/`, checks
the hashed-ID index against committed segment metadata, and rejects missing or
duplicate claims instead of allowing an ambiguous Incident Timeline or Replay
Case. A pre-v1 `events.jsonl` is imported once; if an older BeforeDone process
continues writing that file after migration, reads and writes fail closed until
the version mismatch is resolved.

## Upgrade and complete removal

All three delivery forms share one SemVer release. Upgrade the CLI first, then
refresh whichever single Codex route you selected using the instructions above.
Run `beforedone doctor` in each configured repository after upgrading.

For a complete removal:

1. uninstall the Plugin in the Plugins Directory, remove the two standalone
   skill directories, or run `beforedone setup codex --remove`—whichever route
   you selected;
2. locate the CLI with `command -v beforedone` on macOS/Linux or
   `Get-Command beforedone` in PowerShell, then remove the binary you installed;
3. optionally remove `.git/beforedone` and `.beforedone.yaml` from each
   repository after reviewing what will be deleted;
4. if applicable, run `codex plugin marketplace remove beforedone` to remove the
   Git marketplace source.

## Contributing and license

BeforeDone is licensed under Apache-2.0. Like MIT, it allows commercial use,
modification, and redistribution, but it also gives contributors and users an
explicit patent license and defines contribution, NOTICE, and trademark
boundaries. Contributions use the Developer Certificate of Origin rather than
a CLA; sign commits with `git commit -s`. See
[CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md),
[TRADEMARKS.md](TRADEMARKS.md), and [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
