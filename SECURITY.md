# Security Policy

## Supported versions

Security fixes are applied to the latest released BeforeDone version. Upgrade
the CLI, Plugin, and Skills together because all delivery forms share one
version and release cadence.

## Reporting a vulnerability

Use GitHub Private Vulnerability Reporting on the
[BeforeDone repository](https://github.com/rrrrrredy/beforedone/security/advisories/new).
Do not open a public issue for command injection, path traversal, secret
exposure, receipt forgery, replay execution, or HTML injection findings.

Include a minimal reproduction, affected version and operating system, expected
and observed behavior, impact, and any suggested mitigation. Do not include
real repository secrets or sensitive transcripts. The maintainer will
acknowledge a complete report as soon as practical and coordinate disclosure
after a fix is available.

## Security objective

BeforeDone is a local process guardrail for non-adversarial coding-agent errors:
premature completion, missing checks, failed checks, stale evidence, and
evidence-bounded incident reconstruction. Its normal trust domain is one
repository controlled by the user, with a cooperative or fallible Agent and a
trusted BeforeDone executable.

BeforeDone is not a sandbox, privilege boundary, remote attestation system, or
defense against a malicious process running as the same operating-system user.

## Same-user threat boundary

The receipt key, receipts, logs, event ledger, incidents, and replay cases live
under `.git/beforedone`. Repository policy lives in `.beforedone.yaml`. A
malicious same-user process with write access can:

- read or replace `receipt.key` and compute a valid HMAC;
- edit a receipt and re-sign it;
- change the configured relevant-file patterns or verifier argv;
- run a trivially passing check through the supported CLI; or
- replace other local evidence before it is reviewed.

It can therefore manufacture a semantically self-consistent `PASS`. HMAC
validation catches accidental corruption, unsupported edits, and inconsistent
receipt fields inside the intended workflow; it does not establish an external
trust boundary. Do not use a local receipt as proof against an Agent that has
the authority and intent to rewrite the evidence system itself.

If the Agent is adversarial, add controls outside BeforeDone: a separate OS
identity or container, read-only or protected policy, least-privilege
credentials, protected CI, signed release verification, or an independent
verifier whose key and execution authority the Agent cannot access.

## What v1 does enforce

- Check commands are configured as argv arrays and are executed without shell
  string composition.
- A supported `PASS` receipt records the actual exit code, verifier argv, Git
  commit, relevant-file fingerprint, log digest, timestamps, and BeforeDone
  version.
- A non-zero process exit remains `FAIL` even if its output contains the word
  `PASS`.
- Changes to configured relevant files invalidate an earlier successful
  receipt, including matching Git-ignored files and executable-mode changes.
  Files outside those patterns do not invalidate it.
- Missing evidence and evidence that cannot be classified are reported as
  `INCONCLUSIVE`, not guessed into `PASS` or `FAIL`.
- Imported Replay Cases are treated as evidence. Their recorded argv never
  controls execution.
- `replay analyze` runs no external verifier. `replay verify` is a dry run until
  the user supplies `--execute`, and executable plans come only from the current
  `.beforedone.yaml`.
- Generated incident HTML escapes artifact content and carries a restrictive
  Content Security Policy.
- Configured working directories, runtime paths, receipt logs, and relevant
  file symlinks are constrained to their intended repository or runtime roots.

These checks reduce accidental and opportunistic failure modes. They do not
change the same-user boundary above.

## Commands, worktrees, and network access

Every configured verifier is user-controlled code. Review `.beforedone.yaml`
before `beforedone check` or `beforedone replay verify --execute`.

Replay execution uses a temporary detached Git worktree, but BeforeDone does
not provide network isolation. Verifiers can use the network, environment,
credentials, filesystem access, and process permissions available to the
current user. A detached worktree is repository-state isolation, not a security
sandbox.

## Captured data and redaction

BeforeDone stores its runtime data locally and sends no BeforeDone telemetry.
Codex lifecycle payloads and normalized Adapter imports are redacted and
normalized into bounded event summaries before persistence. An
optional transcript is unstable narrative input, not a trust source, and never
participates in First Observable Divergence. BeforeDone accepts at most 4 MiB,
applies redaction, and persists only a 16 KiB narrative excerpt plus a SHA-256
digest and truncation flag—not the raw transcript. Built-in secret patterns,
configured `capture.redact_patterns`, and output size limits are applied before
supported captured fields are persisted. Normalized events are limited to 256
attributes and 1 MiB after JSON encoding. Event-ledger reads fail closed above
64 MiB instead of silently ignoring a truncated tail. Event writes are
serialized with an operating-system file lock. Each batch is committed as an
immutable JSONL segment through a synced temporary file and an atomic
create-if-absent hard link; readers validate the segment's encoded size, event
count, SHA-256, and unique IDs. After the trusted Git directory is opened,
descendant event-store paths are held through `os.Root` directory handles so a
later descendant directory or junction swap cannot redirect a validated
operation outside `.git/beforedone`. Mutable state and hashed-ID
claims are cache/index data: writers recompute byte and event totals from
immutable segment metadata and fail closed when claim counts disagree. A crash
after segment creation is recovered before the next append. Changes made by an
older writer to a migrated `events.jsonl`, incomplete records, missing claims,
and externally introduced duplicate records all make the store fail closed.
On Unix, BeforeDone also requires the containing directory sync to succeed
before reporting a committed segment, claim, or state update. Windows commonly
does not support flushing ordinary directory handles; on that platform file
contents are flushed before each atomic directory operation, but the final
directory-entry flush remains an operating-system durability boundary.
The immutable create-if-absent commit requires same-directory hard-link support;
on Windows, this means the Git directory must be on NTFS rather than FAT/exFAT.

Redaction is best effort. It cannot guarantee detection of every credential,
personal detail, proprietary value, encoded secret, or sensitive filename.
Receipts intentionally preserve actual verifier argv and working-directory
metadata; output redaction does not rewrite those structural fields. Never put
credentials in command-line arguments, prefer environment-based or native
credential mechanisms, and avoid sensitive filenames where reports may be
shared.
Review logs, event ledgers, incident reports, and Replay Cases before sharing
them. Deleting a report does not revoke copies that were already uploaded or
sent elsewhere.

`reports.retain` limits stored incident directories. Receipts, check logs, and
the event ledger are not automatically pruned in v1. See the README for the
local-data removal procedure.

## Plugin and hook trust

Codex requires users to review non-managed hook definitions. Inspect and trust
the BeforeDone hooks through `/hooks`. Do not enable both the Plugin hooks and
`beforedone setup codex` project hooks in the same repository; Codex loads
matching hook sources together, which would duplicate event capture and Stop
checks.

The Plugin never silently downloads the CLI. Verify the CLI path and version
before trusting a new or changed hook definition.

## Third-party boundaries

The GitHub website, release hosting, Codex, Go toolchain, verifier programs, and
optional installers such as `skills.sh` are separate systems with their own
security and privacy behavior. BeforeDone's no-telemetry statement covers the
BeforeDone CLI, Plugin, and Skills; it does not override data collection by a
third-party installer or hosting provider.
