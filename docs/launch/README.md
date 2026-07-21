# BeforeDone v1.0 launch control room

This directory is the source of truth for launch copy and media acceptance. A
checked box must point to a real public URL or a file captured from a real
BeforeDone run. Mock screenshots and slide-only videos do not count.

## Canonical links

- Product: <https://rrrrrredy.github.io/beforedone/>
- Source: <https://github.com/rrrrrredy/beforedone>
- Release: <https://github.com/rrrrrredy/beforedone/releases/tag/v1.0.0>
- Release evidence: [v1.0.0-release-evidence.md](v1.0.0-release-evidence.md)
- OpenAI Public Plugins Directory: pending identity verification, review, and publication
- Product Hunt: pending submission
- YouTube demo: pending upload

## Required launch assets

- [ ] `../../media/beforedone-demo.mp4`: 60–90 seconds, 1920×1080, real
      terminal and report interaction, English hard captions.
- [ ] `../../media/beforedone-demo.en.srt`: timing corrected against the final MP4.
- [x] `../../media/product-hunt-thumbnail.png`: 240×240, sRGB PNG, under 2 MB.
- [ ] `../../media/gallery/01-stop-hook-block.png`: 1270×760, actual Stop Gate.
- [ ] `../../media/gallery/02-fresh-pass-receipt.png`: 1270×760, actual PASS receipt.
- [ ] `../../media/gallery/03-incident-report.png`: 1270×760, actual HTML report.
- [ ] `../../media/gallery/04-replay-verify-dry-run.png`: 1270×760, actual replay plan.
- [ ] `../../media/youtube-cover.png`: 1280×720.
- [x] Public sample Incident Report linked from the site:
      <https://rrrrrredy.github.io/beforedone/demo/incident.html>.

Do not create empty placeholders for these files. Missing means missing.

## Go/no-go evidence

- [x] The selected release mode is closed: either all GitHub workflows are
      green, or Actions are disabled and the documented local CI, security,
      Pages, and release audits pass. Evidence: [v1.0.0 release audit](v1.0.0-release-evidence.md).
- [x] `v1.0.0` release contains six archives, checksums, six SPDX SBOMs, and
      Homebrew/Scoop manifests. GitHub provenance is required only for the
      Actions release mode and is not claimed for this manual release.
- [ ] CLI installs and runs from public artifacts on clean Windows, macOS, and
      Linux hosts.
- [x] Plugin installs and enables from the public Git marketplace in an
      isolated clean Codex setup; cached plugin files match the source.
- [x] Both standalone skills install with the official Codex skill installer
      from the public `v1.0.0` tag; installed files match the tagged Git blobs.
- [x] Both public skills.sh discovery pages resolve anonymously. This is a
      discovery check, not a claim that the third-party `npx skills` client ran.
- [x] The exact public Skills-only ZIP installs and enables in a fresh isolated
      Codex home. Codex discovers both namespaced Skills, with zero Hook, MCP,
      or App components.
- [ ] The Skills-only package passes OpenAI review and is published in the
      Public Plugins Directory; its listing does not claim automatic Stop.
- [x] Website works anonymously at `/beforedone/` on desktop and mobile; the
      complete guide and `#fullmd` route are also live.
- [ ] All screenshots are recaptured after the final UI/CLI copy freeze.
- [ ] Final MP4 shows a real blocked completion, real failing check, real fix,
      fresh receipt, Incident Report, and replay dry-run.
- [ ] YouTube URL is public or unlisted and embedded successfully on the site.
- [ ] Product Hunt fields, gallery, Maker Comment, and final links are reviewed.
- [ ] Product Hunt launch is live, not merely saved as a draft.

Use [demo-runbook.md](demo-runbook.md), [media-checklist.md](media-checklist.md),
[youtube.md](youtube.md), and [product-hunt.md](product-hunt.md) in that order.

## Remaining non-video gates

- Re-capture the four gallery images from final-release terminal and browser
  windows during the final media session. Do not open repeated capture windows
  during engineering work; the retained rehearsal renderings are not acceptance
  proof.
- Exercise the public Linux and macOS archives on native or trusted clean hosts;
  the public Windows amd64 archive has already passed download, hash, version,
  and fixture checks.
- Preserve the isolated Git Marketplace Plugin and official standalone Skills
  installation evidence; both public installation routes now pass. The
  external skills.sh discovery pages and the exact Skills-only ZIP load test
  also pass.
- Complete identity verification and third-party review for the Skills-only
  OpenAI Public Plugins Directory package.

The final MP4 remains deliberately last. YouTube upload and Product Hunt launch
follow it because both require the final video URL and user-account approval.
