# Contributing to BeforeDone

BeforeDone accepts focused issues and pull requests that preserve its evidence
and safety invariants. Discuss large behavioral changes in an issue first.

## Development

1. Install Go 1.26 or newer.
2. Run `go test ./...`.
3. Run `go vet ./...`.
4. Validate both skills and the Codex plugin using the repository checks.

Changes to receipts, fingerprints, hook decisions, replay execution, redaction,
or public schemas require regression tests and an adversarial review.

User-facing README, site, install, or release-documentation changes must stay
in sync. With GitHub Actions disabled, maintainers publish the matching static
files to `gh-pages` and verify the public routes before calling the update done.

## Developer Certificate of Origin

This project uses the Developer Certificate of Origin 1.1 instead of a CLA.
Sign every commit with `git commit -s` to certify that you have the right to
submit the contribution under Apache-2.0.

The full DCO is available at https://developercertificate.org/.
