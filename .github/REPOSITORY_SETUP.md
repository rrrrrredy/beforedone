# GitHub repository setup

These settings cannot be expressed safely by files in the repository. Apply
them after `rrrrrredy/beforedone` is public.

## Before the first release

- Keep the default `GITHUB_TOKEN` permission read-only. Build and review every
  release candidate locally, then use GitHub CLI to create one final Release
  after the notes, tag, assets, checksums, and SBOMs are ready. The repository
  Release workflow verifies an already public formal Release and must never
  receive permission to create or mutate one.
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

The formal local release path does not claim GitHub OIDC provenance. Its public
verification boundary is the tag, release asset matrix, checksums, SBOMs, the
read-only cross-platform verification workflow, and the documented quality-gate
record. The current v1.0.2 record is
[`docs/releases/v1.0.2-release-evidence.md`](../docs/releases/v1.0.2-release-evidence.md).
