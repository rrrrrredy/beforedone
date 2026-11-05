---
name: verify-before-done
description: Run configured BeforeDone checks and confirm that their results still match the current files. Use when the user requests BeforeDone verification or the repository already uses BeforeDone for completion evidence.
---

# Verify Before Done

Use the BeforeDone CLI as the only authority for a valid Completion Receipt. Raw test output, an agent's summary, or the word `PASS` in a log is supporting material, not a receipt.

## Boundaries

- Work in the intended Git worktree and preserve the user's stated scope.
- Treat this standalone skill as a manual workflow. It cannot install a Stop hook or prevent Codex from ending a turn; lifecycle enforcement comes from the BeforeDone Git Marketplace Plugin or project-local hooks installed with `beforedone setup codex`.
- Never fabricate, edit, or relabel a receipt. Only `beforedone check` may create one.
- A fresh receipt records the configured verifier's result for its declared files. Report which task acceptance criteria those checks cover, and identify any specific gap. Keep the verifier's checks intact when reporting PASS.
- A prospective study registration, Pattern Registry, Skill Impact Ledger, or security scan is not Completion Receipt evidence. Let the research or memory system preserve those records; use BeforeDone only to prove that configured checks pass for the final relevant-file fingerprint.
- Do not silently install or download the CLI. If `beforedone` is unavailable, report that fact and offer `go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest` for explicit approval.

## Workflow

1. Read the repository's verification configuration.
   - Work from the intended Git worktree and read `.beforedone.yaml`.
   - Reuse its check IDs, argv, and relevant-file scope. All required checks need current evidence at completion.
   - If configuration is absent and setup is requested, connect the project's existing verification command with `beforedone init --files '<pattern>' -- <command> [args...]`. Repeat `--files` for source, tests, fixtures, and build inputs that affect the check. Bare `init` defaults to `go test ./...` in Go modules; other projects must supply a verifier. On older CLIs, run `beforedone init` and replace any generated placeholder command before running a check.
   - Use `beforedone doctor` during setup or when an environment problem needs diagnosis; it is not a mandatory step on every task.

2. Run the checks that need new evidence.
   - Run `beforedone check <check-id>` for checks affected by the task, or whose evidence is missing, stale, or failed.
   - Run the verifier through BeforeDone once; do not first run the same command directly just to run it again for a receipt.
   - Preserve exit codes and output. After a relevant source change, rerun the affected check.

3. Confirm completion once.
   - For a manual workflow, run `beforedone gate`. It evaluates every required check against the current files without rerunning the verifiers.
   - If checking whether previous results can be reused, start with `gate` and run only checks that need fresh evidence. A fresh PASS needs no rerun.
   - When an active lifecycle integration supplies the final gate, a separate manual gate is optional. When reporting a gate result, use its evidence verdict.
   - Use `beforedone receipt <check-id>` only when detailed receipt inspection is needed; it is not an extra completion step.

4. Report the result.
   - Report PASS only when all required checks have fresh PASS receipts.
   - Report FAIL when a configured check ran and failed.
   - Report INCONCLUSIVE when BeforeDone cannot prove either state, including missing configuration, missing receipts, or unusable evidence.
   - Include the checks run or reused, verdicts, and receipt paths already emitted by BeforeDone. Explain the next step for a non-PASS result. Gate permission to stop is not itself a PASS verdict.

## Exit codes

- `0`: command succeeded or verification PASS
- `1`: verification FAIL
- `2`: verification INCONCLUSIVE
- `64`: invalid invocation or configuration
- `70`: internal error

Never convert `1`, `2`, `64`, or `70` into a successful completion claim.
