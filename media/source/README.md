# BeforeDone demo source evidence

The retained structured artifacts below came from a real local run and make the
functional claims auditable. They are rehearsal/source evidence, not acceptance
proof for the current `media/beforedone-demo.mp4` or gallery images. Final launch
media must be recaptured from visible terminal and browser windows after the
release commit and public URLs exist, as required by `docs/launch/README.md`.

Run ID: `bd-demo-20260717`

- CLI SHA-256: `a2c53de6b1f0fa0cc55da74b4ab6dfe9b96e83968b72308879ede2f7bc6b51b2`
- Demo repository commit: `0031474c33f4587fb70bc5a73d6b712aa220d2ef`
- Platform: Windows amd64, PowerShell, Go 1.26.5
- Stop screen: the Codex plugin's real PowerShell hook wrapper received
  `raw/stop-no-evidence.json` on stdin and returned the retained block decision.
- Check screens: the signed FAIL and fresh PASS receipts are retained under
  `artifacts/`, along with the complete failed-check log.
- Incident screen: `artifacts/incident-report.html` is the self-contained HTML
  written by the CLI in this run. Its JSON and Replay Case are retained beside it.
- Replay screens: the terminal text is the real human output of `replay analyze`
  and the default, non-executing `replay verify` dry run for that Replay Case.

The renderer only removes long absolute path prefixes with `incident-...` where
needed for legibility. IDs, verdicts, fingerprints, exit codes, verifier output,
and safety statements are preserved exactly. `frames.json` is the media timeline
and points to the retained source for every scene.

Reproduction sequence:

```powershell
beforedone init
# Send raw/stop-no-evidence.json to the plugin hook wrapper.
beforedone check unit                 # after changing a + b to a - b
beforedone incident --correction "The test proves Add returns -2. Fix addition before claiming completion."
beforedone check unit                 # after restoring a + b
beforedone receipt unit
beforedone replay analyze .git/beforedone/latest-replay-case.json
beforedone replay verify .git/beforedone/latest-replay-case.json
```

Capture the required real windows with the scripts under `source/capture/`, put
the resulting clips under `source/raw-clips/`, then compose and validate them
with `scripts/build_demo_media.ps1`. The separate
`scripts/build_demo_media_fallback.js` is rehearsal-only and cannot satisfy the
launch media checklist.
