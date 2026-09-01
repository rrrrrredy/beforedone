# Evolution study boundary

Status: accepted for the current product boundary

## Decision

BeforeDone proves verifier freshness for a final repository state. It will not
store or interpret prospective study plans, Pattern Registries, Skill Impact
Ledgers, model-transfer results, or accumulated experience.

The products remain connected through files with separate authority:

- Agent Memory may preserve an exact prospective study plan before execution;
- Runtime Evolution Workbench may retain patterns, counterexamples, candidate
  decisions, and an append-only impact chain;
- Skill Security Guard may bind a static scan to exact candidate bytes;
- BeforeDone may verify configured checks against the final relevant-file
  fingerprint.

None of the first three records can create, refresh, or upgrade a BeforeDone
PASS. A BeforeDone PASS cannot establish that an evolution method improved task
quality, reduced cost, transferred across models, or caused an observed effect.

## Research artifact use

A research repository may declare its frozen inputs, analysis scripts, result
tables, paper source, and final PDF as `relevant_files`. In that case a fresh
Completion Receipt establishes only that the configured reproducibility and
consistency checks passed for those exact files. It does not certify the remote
model identity, hidden provider behavior, experimental independence, or the
truth of a causal interpretation.

Prospective ordering remains outside BeforeDone. The study plan must be frozen
and registered before the first eligible run by the system that owns study
provenance. Running BeforeDone at the end cannot repair a retrospectively
written plan.

## Why this boundary is useful

Combining these roles would let an outcome ledger masquerade as completion
proof or let a fresh build receipt masquerade as research efficacy. Keeping the
authorities separate makes each statement falsifiable:

- study registration answers what was fixed in advance;
- evolution records answer what changed and why;
- security attestation answers which bytes were scanned;
- Completion Receipt answers whether final configured checks are fresh.

This decision requires no Receipt schema change.
