# Release evidence

This directory contains the versioned manual release audits for BeforeDone.
Each record states exactly what was verified and keeps native-runtime or
third-party-review exceptions explicit.

## Unreleased: simpler setup and daily verification

- Connect an existing verifier with `beforedone init --files <pattern> -- <command> [args...]`.
  Repeat `--files` to describe its inputs. The command is stored as argv and is
  not executed during setup. Existing configuration is kept; edit it to change
  commands or file scope.
- The text gate output now lists each required check and its freshness.
  Daily guidance uses `check` followed by one completion gate, reuses current
  results, and makes detailed `receipt` inspection optional.
- README, installation guidance, site source, and canonical/plugin skill copies
  describe the same workflow. Incident and replay tools remain available as
  advanced features. Gate decisions, receipt schemas, and integrations retain
  their existing behavior.

These changes are available from the development source and documented on the
matching site. Install with `go install github.com/rrrrrredy/beforedone/cmd/beforedone@main`
until the next release. The latest published CLI is
[v1.1.1](https://github.com/rrrrrredy/beforedone/releases/tag/v1.1.1); its release
assets do not accept the new init options.

## Published release records

- [v1.0.2](v1.0.2-release-evidence.md) records the v1.0.2 engineering release.
- [v1.0.1](v1.0.1-release-evidence.md) is the previous patch release.
- [v1.0.0](v1.0.0-release-evidence.md) is the initial public release.

A promotional video was considered and deliberately removed from the project
completion boundary. The public Incident example and reproducible review
fixtures provide inspectable evidence without keeping rehearsal captures and
recording machinery in `main`.

The v1.0.0 and v1.0.1 records preserve the fact that a Skills-only OpenAI
Directory artifact was built and verified with those releases. The maintainer
later canceled that distribution path. BeforeDone is not listed in the OpenAI
Public Plugins Directory, and v1.0.2 does not publish that artifact.
