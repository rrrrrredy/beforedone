# Secondary launch drafts

Prepare these after Product Hunt assets are final. Do not auto-post them.

## Show HN

**Title:** `Show HN: BeforeDone – Evidence gates and incident replay for coding agents`

**Body:**

```text
I built BeforeDone to separate an agent's completion claim from fresh evidence
about the current code. It is a local-only Go CLI plus a Codex Plugin and two
standalone Skills.

Checks run as argv arrays through BeforeDone and produce receipts bound to the
relevant-file fingerprint. The Stop Hook can reject one premature completion
attempt without looping forever. Failed runs become self-contained incident
reports and replay cases; the analysis is limited to observable evidence and
does not claim to recover hidden reasoning.

Repo: https://github.com/rrrrrredy/beforedone
Demo: https://rrrrrredy.github.io/beforedone/

I would appreciate adversarial examples around stale evidence, false blocks,
and cases where First Observable Divergence is too weak to be useful.
```

## X

```text
“The coding agent finished” is a claim. “The current code passed the required
checks” is evidence.

BeforeDone keeps those separate: fresh code-bound receipts, a Codex completion
gate, and replayable incident reports. Local-only and open source.

https://rrrrrredy.github.io/beforedone/
```

## Dev.to

Draft title: `Why a coding agent's PASS should expire when the code changes`

Use a technical article built around the fingerprint invalidation model, the
one-retry Stop Gate, and a real Incident Report. Do not republish the Product
Hunt description as an article.

## Reddit

Only post in communities whose self-promotion rules permit it. Lead with the
technical problem and repository, disclose that the author built the project,
and ask for failure fixtures. Do not coordinate votes or cross-post identical
copy simultaneously.
