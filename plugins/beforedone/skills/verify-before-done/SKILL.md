---
name: verify-before-done
description: Require fresh, repository-state-bound BeforeDone evidence before reporting a coding task complete. Use when Codex needs to verify an implementation, run configured completion checks, inspect an evidence receipt, distinguish PASS from FAIL or INCONCLUSIVE, or confirm that earlier test evidence is still valid after files changed.
---

# Verify Before Done

Use the BeforeDone CLI as the only authority for a valid Completion Receipt. Raw test output, an agent's summary, or the word `PASS` in a log is supporting material, not a receipt.

## Boundaries

- Work in the intended Git worktree and preserve the user's stated scope.
- Treat this standalone skill as a manual workflow. It cannot install a Stop hook or prevent Codex from ending a turn; lifecycle enforcement comes from the BeforeDone Git Marketplace Plugin or project-local hooks installed with `beforedone setup codex`.
- Never fabricate, edit, or relabel a receipt. Only `beforedone check` may create one.
- Do not silently install or download the CLI. If `beforedone` is unavailable, report that fact and offer `go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest` for explicit approval.

## Workflow

1. Confirm the repository and CLI.
   - Run `git rev-parse --show-toplevel` and use that worktree as the verification root.
   - Run `beforedone doctor`.
   - If the command or repository is unavailable, stop and report the exact failure.

2. Confirm the verification contract.
   - Read `.beforedone.yaml` and identify every required check whose relevant-file patterns cover the changed work.
   - Do not invent a check ID or replace configured argv with a convenient command.
   - If the file is absent and the user asked to set up BeforeDone, run `beforedone init`, then review the generated configuration before using it. Otherwise report that verification is not configured.

3. Generate fresh evidence.
   - Run `beforedone check <check-id>` once for each required check.
   - Preserve the command's exit code and output. Do not pipe, rewrite, or post-process output in a way that can hide failure.
   - After any relevant source change, rerun the affected check; an earlier PASS is stale by design.

4. Inspect the receipt state.
   - Run `beforedone receipt <check-id>` for every required check.
   - Confirm that each required receipt is bound to the current relevant-file fingerprint and reports the expected check ID.
   - Treat missing, malformed, or unreadable evidence as INCONCLUSIVE, never as PASS.

5. Report the result.
   - Report PASS only when all required checks have fresh PASS receipts.
   - Report FAIL when a configured check ran and failed.
   - Report INCONCLUSIVE when BeforeDone cannot prove either state, including missing configuration, missing receipts, or unusable evidence.
   - Include the check IDs, commands run, verdicts, and receipt paths emitted by BeforeDone. State what must happen next for every non-PASS result.

## Exit codes

- `0`: command succeeded or verification PASS
- `1`: verification FAIL
- `2`: verification INCONCLUSIVE
- `64`: invalid invocation or configuration
- `70`: internal error

Never convert `1`, `2`, `64`, or `70` into a successful completion claim.
