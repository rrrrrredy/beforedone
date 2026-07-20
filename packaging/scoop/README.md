# Scoop bucket bootstrap

1. Create the public repository `rrrrrredy/scoop-bucket` with a `main` branch.
2. Copy `bucket-update.yml` to `.github/workflows/update.yml` in that repository.
3. In the repository's Actions settings, allow `GITHUB_TOKEN` read/write access.
4. Run **Update BeforeDone Scoop manifest** and enter a published tag such as
   `v1.0.0`.
5. Confirm `beforedone.json` was committed at the repository root, then test on
   clean Windows amd64 and arm64 hosts:

```powershell
scoop bucket add beforedone https://github.com/rrrrrredy/scoop-bucket
scoop checkup
scoop install beforedone
beforedone doctor
scoop uninstall beforedone
```

Scoop expects manifests at the bucket root. Do not move `beforedone.json` into
a subdirectory.
