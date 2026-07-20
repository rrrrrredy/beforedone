# GitHub repository setup

These settings cannot be expressed safely by files in the repository. Apply
them after `rrrrrredy/beforedone` is public.

## Before the first release

- Under **Actions > General > Workflow permissions**, allow read and write so
  the built-in `GITHUB_TOKEN` can create releases and attestations.
- Under **Pages**, select **GitHub Actions** as the source. Do not configure a
  custom domain and do not add a `CNAME` file.
- Enable the dependency graph, Dependabot alerts, secret scanning, validity
  checks, and push protection. These are free for the public repository.
- Add a `main` ruleset requiring pull requests and the following status checks:
  `Quality gates`, all three `Test on ...` checks, `CodeQL (Go)`, `Gitleaks
  history scan`, and `Dependency Review` when a dependency changes.
- Restrict tag creation matching `v*` to the maintainer.
- Set the About website to `https://rrrrrredy.github.io/beforedone/` and add the
  topics `codex`, `coding-agent`, `developer-tools`, `go`, and `open-source`.

## Package repositories

Create public `rrrrrredy/homebrew-tap` and `rrrrrredy/scoop-bucket` repositories.
Each needs Actions read/write permission only for its own built-in token. Copy
the workflow templates from `packaging/` as documented there; no PAT or paid
service is required.

## Release verification

For a downloaded release archive, verify provenance with GitHub CLI:

```console
gh attestation verify beforedone_1.0.0_linux_amd64.tar.gz --repo rrrrrredy/beforedone
```

Also verify the archive's SHA-256 value against `checksums.txt` before running
the binary.
