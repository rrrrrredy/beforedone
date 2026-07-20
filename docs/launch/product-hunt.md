# Product Hunt launch package

## Listing fields

- **Name:** BeforeDone
- **Tagline:** Make coding agents prove they're done.
- **Primary URL:** https://rrrrrredy.github.io/beforedone/
- **GitHub:** https://github.com/rrrrrredy/beforedone
- **Pricing:** Free
- **Topics:** Developer Tools, Open Source, Artificial Intelligence
- **Video:** final full YouTube URL; do not use a `youtu.be` placeholder

## Description

BeforeDone is a local proof layer for coding agents. It blocks premature
completion when required checks are missing, failed, or stale; binds fresh test
evidence to the code that actually ran; and turns failed sessions into
replayable incident reports with an evidence-supported First Observable
Divergence. It ships as a Go CLI, a Codex Plugin, and standalone Skills. No
account, cloud backend, telemetry, or paid plan.

### 260-character fallback

BeforeDone makes coding agents prove completion with fresh checks bound to the
current code. Its Codex gate blocks stale claims once, while Incident Lab turns
failed runs into replayable, evidence-backed reports. Local-only, open source,
and free.

## Maker Comment draft

Hi Product Hunt — I built BeforeDone because “the agent said it was done” and
“the current code is proven to work” are different facts.

BeforeDone makes that distinction executable. A required check only counts when
it runs through the CLI and produces a fresh receipt bound to the relevant
files. The Codex Plugin can stop one premature completion attempt and tell the
agent exactly which evidence is missing. When a run goes wrong, Incident Lab
builds a local timeline, claim/evidence matrix, and the earliest divergence the
available evidence can actually support. It does not pretend to reconstruct
hidden chain of thought.

The whole v1 is open source and local-only: Go CLI, Codex Plugin, standalone
Skills, Adapter Kit, and self-contained HTML reports. There is no account,
telemetry, MCP server, hosted backend, or paid dependency.

I would especially value examples where BeforeDone blocks too aggressively,
lets a stale claim through, or cannot locate a useful divergence. Those are the
cases that will make the evidence model better.

Website: https://rrrrrredy.github.io/beforedone/
Source: https://github.com/rrrrrredy/beforedone

## First comment / FAQ answers

**Does it inspect hidden reasoning?**

No. It uses observable events, Git state, verifier output, receipts, and user
corrections. First Observable Divergence is deliberately evidence-bounded.

**Does it upload code or transcripts?**

No. v1 is local-only and has no telemetry or hosted backend. Transcript input
is optional and treated as unstable narrative context.

**Why three forms?**

The CLI is the fact source. The Codex Plugin adds automatic lifecycle hooks.
The standalone Skills provide a manual workflow when installing a plugin is not
appropriate.

**Does the OpenAI Plugins Directory edition enforce the Stop Gate?**

No. Because BeforeDone v1 intentionally has no MCP server, its public Directory
submission is Skills-only and provides the same manual workflows as the
standalone Skills Pack. The Hook-enabled Plugin is installed from the public
Git Marketplace and requires the local CLI.

**Which agents are supported?**

Codex is the only officially supported v1 integration. The public Adapter Kit
is for later integrations; it is not a compatibility claim.

## Submission gate

- Use the four real product gallery images and the 240×240 thumbnail.
- Test the website and GitHub URLs while logged out.
- Paste the final YouTube URL and verify its preview.
- Do not list CLI, Plugin, and Skills as separate products.
- Do not post or schedule until the user approves the final preview.
- After going live, replace the pending Product Hunt URL in the launch README.
