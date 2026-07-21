# GitHub repository setup

These settings cannot be expressed safely by files in the repository. Apply
them after `rrrrrredy/beforedone` is public.

## Before the first release

- Choose one release mode. If Actions are available, allow read and write so
  the built-in `GITHUB_TOKEN` can create releases and attestations. If Actions
  are unavailable or intentionally disabled, keep them disabled, run every
  quality gate locally, publish the staged static site from the `gh-pages`
  branch, and upload release artifacts with GitHub CLI.
- Under **Pages**, select **GitHub Actions** for the workflow mode or the root
  of `gh-pages` for the no-Actions mode. Do not configure a custom domain and
  do not add a `CNAME` file.
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

Always verify a downloaded archive's SHA-256 value against `checksums.txt`
before running the binary. Each archive also ships with an SPDX SBOM.

Releases produced by the Actions workflow additionally have GitHub build
provenance and can be verified with GitHub CLI:

```console
gh attestation verify beforedone_1.0.0_linux_amd64.tar.gz --repo rrrrrredy/beforedone
```

Manual no-Actions releases do not claim GitHub OIDC provenance; their public
verification boundary is the tag, release asset matrix, checksums, SBOMs, and
the documented local quality-gate record.
