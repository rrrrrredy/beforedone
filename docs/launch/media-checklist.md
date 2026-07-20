# Media production and acceptance

## Capture once, derive carefully

Record the 1920×1080 master first. Capture gallery images separately from the
same final build rather than upscaling video frames when that would blur text.
Decorative artwork may be generated, but every product claim must be backed by
visible real output.

## Product Hunt thumbnail — 240×240

- High-contrast BeforeDone mark or `BD` monogram.
- No terminal screenshot and no small body copy.
- Must remain recognizable at 60×60.
- Export PNG, sRGB, under 2 MB.

## Gallery — 1270×760 each

1. **Stop Gate:** Codex Stop Hook response with the missing/stale `unit` check.
2. **Fresh Receipt:** CLI receipt with PASS, check ID, fingerprint, and time.
3. **Incident Lab:** HTML Timeline and First Observable Divergence.
4. **Replay:** CLI showing `replay verify` as a dry-run plan.

For each image:

- Capture at native scale; do not reconstruct terminal output in a design tool.
- Crop personal chrome while keeping enough application context to prove the
  surface is real.
- Add at most one short annotation outside the product UI.
- Use a consistent background and safe margin; never obscure evidence.
- Verify dimensions, contrast, spelling, URL, version, and absence of secrets.

## Video acceptance

- Duration is 60–90 seconds and resolution is exactly 1920×1080.
- H.264 video is broadly playable; no paid codec or hosted asset is required.
- The first product interaction starts by second six.
- At least half of the runtime shows live core functionality.
- No slide sequence, fake cursor, stock terminal, or generated UI is used.
- Hard captions match the separate `.srt` and stay inside safe margins.
- Final three seconds show both:
  `rrrrrredy.github.io/beforedone/` and `github.com/rrrrrredy/beforedone`.

## Evidence ledger

For every final image/video, record the release tag, commit SHA, capture date,
source OS, and exact fixture in the launch PR description. This makes later
recapture decisions objective when product output changes.
