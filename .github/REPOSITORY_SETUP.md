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
- Enable the dependency graph, Dependabot alerts and security updates, secret
  scanning, and push protection. Enable secret validity checks when GitHub
  exposes that setting for the account; record an explicit exception when the
  API leaves it disabled.
- In Actions mode, add a `main` ruleset requiring pull requests and the
  following status checks: `Quality gates`, all three `Test on ...` checks,
  `CodeQL (Go)`, `Gitleaks history scan`, and `Dependency Review` when a
  dependency changes.
- In no-Actions mode, do not require workflow status checks: disabled workflows
  can never satisfy them. Use pull-request review where practical and attach the
  checked-in local release audit to every manual release instead.
- Where repository rulesets are available, restrict creation, update, and
  deletion of tags matching `v*` to the maintainer. Never rewrite an already
  published release tag merely to change its tag object type.
- Set the About website to `https://rrrrrredy.github.io/beforedone/` and add the
  topics `codex`, `coding-agent`, `developer-tools`, `go`, and `open-source`.

## Package repositories

Create public `rrrrrredy/homebrew-tap` and `rrrrrredy/scoop-bucket` repositories
only when those installation routes are ready to be supported. With Actions
available, each can use read/write permission only for its own built-in token.
With Actions unavailable, keep them disabled and commit the verified manifests
from the public BeforeDone Release directly from an audited local checkout.
Neither path requires a PAT or paid service.

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
the documented local quality-gate record. The v1.0.0 record is
[`docs/launch/v1.0.0-release-evidence.md`](../docs/launch/v1.0.0-release-evidence.md).
