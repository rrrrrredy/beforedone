# BeforeDone v1.0 launch control room

This directory is the source of truth for launch copy and media acceptance. A
checked box must point to a real public URL or a file captured from a real
BeforeDone run. Mock screenshots and slide-only videos do not count.

## Canonical links

- Product: <https://rrrrrredy.github.io/beforedone/>
- Source: <https://github.com/rrrrrredy/beforedone>
- Releases: <https://github.com/rrrrrredy/beforedone/releases>
- OpenAI Public Plugins Directory: pending identity verification, review, and publication
- Product Hunt: pending submission
- YouTube demo: pending upload

## Required launch assets

- [ ] `../../media/beforedone-demo.mp4`: 60–90 seconds, 1920×1080, real
      terminal and report interaction, English hard captions.
- [ ] `../../media/beforedone-demo.en.srt`: timing corrected against the final MP4.
- [ ] `../../media/product-hunt-thumbnail.png`: 240×240.
- [ ] `../../media/gallery/01-stop-hook-block.png`: 1270×760, actual Stop Gate.
- [ ] `../../media/gallery/02-fresh-pass-receipt.png`: 1270×760, actual PASS receipt.
- [ ] `../../media/gallery/03-incident-report.png`: 1270×760, actual HTML report.
- [ ] `../../media/gallery/04-replay-verify-dry-run.png`: 1270×760, actual replay plan.
- [ ] `../../media/youtube-cover.png`: 1280×720.
- [ ] Public sample Incident Report linked from the site.

Do not create empty placeholders for these files. Missing means missing.

## Go/no-go evidence

- [ ] CI, Security, dependency review, Pages, and release workflows are green.
- [ ] `v1.0.0` release contains six archives, checksums, six SPDX SBOMs,
      Homebrew/Scoop manifests, and verifiable provenance.
- [ ] CLI installs and runs from public artifacts on clean Windows, macOS, and
      Linux hosts.
- [ ] Plugin installs from the public Git marketplace in a clean Codex setup.
- [ ] Both standalone skills install from the public repository and skills.sh.
- [ ] The Skills-only package passes OpenAI review and is published in the
      Public Plugins Directory; its listing does not claim automatic Stop.
- [ ] Website works anonymously at `/beforedone/` on desktop and mobile.
- [ ] All screenshots are recaptured after the final UI/CLI copy freeze.
- [ ] Final MP4 shows a real blocked completion, real failing check, real fix,
      fresh receipt, Incident Report, and replay dry-run.
- [ ] YouTube URL is public or unlisted and embedded successfully on the site.
- [ ] Product Hunt fields, gallery, Maker Comment, and final links are reviewed.
- [ ] Product Hunt launch is live, not merely saved as a draft.

Use [demo-runbook.md](demo-runbook.md), [media-checklist.md](media-checklist.md),
[youtube.md](youtube.md), and [product-hunt.md](product-hunt.md) in that order.
