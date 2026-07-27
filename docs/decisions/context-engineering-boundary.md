# Context engineering boundary

Status: accepted for the current v1 product boundary
Source reviewed: [The new rules of context engineering for Claude 5 generation models](https://claude.com/blog/the-new-rules-of-context-engineering-for-claude-5-generation-models), July 24, 2026

## Decision

BeforeDone will not make the complete Agent context part of Completion Receipt
validity. It will not add a first-class context manifest or long-task state
manager in v1.

The useful lesson from the source article is narrower: keep Agent-facing
instructions small, avoid conflicting duplication, and enforce mechanical
invariants through interfaces and tests. The two canonical BeforeDone Skills
already follow that structure and their plugin copies are mechanically
synchronized and checked in CI.

## Receipt boundary

A current Receipt is bound to:

- the check ID;
- the verifier argv;
- the repository-relative working directory;
- the configured relevant-file patterns and the matching repository state,
  through the relevant-file fingerprint;
- the observed command result, timestamps, bounded output summaries, and the
  referenced log digest;
- the local repository runtime key through the Receipt HMAC.

The Git commit and BeforeDone version are recorded metadata. The freshness
predicate does not bind arbitrary environment variables, tool binaries,
network state, timeout, `required`, capture settings, the model, system or user
prompts, AGENTS.md, Skills, tool schemas, Memory, or hidden reasoning.

Changing Agent context does not change the historical fact that a configured
command passed on one relevant repository state. Binding all such context would
create false staleness without strengthening that fact. If an instruction,
rubric, prompt template, or tool manifest is itself an input to the product
being verified, the maintainer can include its repository file in
`relevant_files`.

## Adopt / Defer / Reject

### A. Bind Agent context to Completion Receipts — Reject

The proposed metadata is upstream of the code and verifier observation, not an
input to the verifier result by default. Receipt validity remains limited to
the explicit verifier contract and relevant repository state.

### B. Add a bounded context manifest — Defer

Model and harness provenance can help reproduce an Agent experiment, but
BeforeDone replay reruns configured verifiers rather than the Agent. The
product cannot reliably prove which system instructions, Skills, tool schemas,
or Memory entries a harness actually loaded. A self-reported manifest would
therefore be narrative provenance, not evidence for PASS or First Observable
Divergence.

The normalized event contract already permits bounded, best-effort-redacted
adapter attributes, and the Codex adapter currently records available source
metadata such as `model` as adapter-supplied attributes. Ordinary private path
components are not automatically anonymized, so these attributes still require
review before sharing. This existing, explicitly limited surface is sufficient
for a real adapter or incident to demonstrate a missing use case before a new
public data model is introduced. Reconsider only when incident evidence shows
that a specific bounded field materially improves explanation or replay. Never
store raw prompts, Memory, transcripts, or chain of thought by default.

Study-runner cache qualification is a separate concern. A research harness may
bind model, runtime, prompt, tool, and task-manifest digests to experimental
runs without changing BeforeDone Receipt semantics.

### C. Store long-task objective and progress — Reject

`objective`, `phase`, `last_verified_state`, `open_risks`, and `next_action`
belong to the Agent harness, study runner, or task window. BeforeDone does not
own task planning and will not become a general state or Memory manager.

### D. Restructure the Skills — Adopt the principle; keep the current shape

Each canonical Skill is a short workflow containing the product's fragile
invariants, verdict semantics, and safety boundary. Splitting these 54-line
files into references would add navigation without removing meaningful
context. Canonical and plugin copies are byte-for-byte synchronized by
`scripts/sync_plugin_skills.py --check`, and CI plus release validation enforce
that contract.

No Skill text is removed. A stale website Receipt field name is corrected, and
regression tests make the following material boundary observable:

- relevant state and verifier argv or working-directory changes invalidate old
  evidence;
- an unrelated Agent instruction file does not invalidate a Receipt unless it
  is declared relevant;
- retained adapter metadata remains bounded and receives the existing
  best-effort secret redaction before persistence;
- adapter-supplied context metadata cannot create or upgrade PASS;
- incident classification does not infer hidden reasoning from metadata.

## Compatibility

This decision makes no public schema or runtime data-model change.
`schema_version: 1` Receipt, Event, Incident, and Replay Case files remain
compatible. Missing context metadata has no effect on an otherwise valid
Receipt.
