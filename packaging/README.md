# BeforeDone release and package-manager strategy

The canonical release is the GitHub Release created from a final `vMAJOR.MINOR.PATCH`
tag. It contains six archives, SHA-256 checksums, one SPDX 2.3 SBOM per archive,
and generated Homebrew/Scoop manifests. The current release path is locally
built and must say that it has no GitHub OIDC provenance rather than implying
that it does. The repository workflow never creates, edits, hides, or republishes
a GitHub Release; it verifies an already public formal release.

## Supported release matrix

| OS | Architectures | Archive |
| --- | --- | --- |
| macOS | amd64, arm64 | `.tar.gz` |
| Linux | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

All binaries are statically compiled with `CGO_ENABLED=0`. The release does not
require a paid BeforeDone dependency or a cross-repository credential.

## Formal-only release procedure

1. Confirm the `CI`, `Security`, `Dependency Review`, and Pages checks are green
   on `main`.
2. Confirm the plugin manifest, standalone skills, and CLI all report the same
   version that will be tagged.
3. Keep release notes and every candidate asset in a local release directory;
   no GitHub object is used as a staging surface.
4. Run the full Go suite, distribution validator, workflow lint, and a full
   Git-history secret scan locally. Record the exact tool versions and source
   commit used.
5. Create the annotated final SemVer tag locally, for example `v1.1.0`. Build
   the six archives, checksums, six SPDX SBOMs, and package manifests with the
   pinned GoReleaser and Syft versions. GoReleaser is configured with release
   upload disabled.
6. Verify all local digests, inspect every archive, run the native binaries,
   scan the final notes and asset names for credentials or local paths, and
   confirm the tag commit is reachable from `main`.
7. Push the tag, verify tag-based `go install`, then create the final GitHub
   Release once with the prepared title, notes, complete asset set, and latest
   marker. Do not use a public GitHub object to hold intermediate content.
8. Let `Public release verification` anonymously download and test the formal
   assets on Windows, macOS, and Linux. It has read-only repository permission
   and cannot change Release state.
9. In the two package repositories, run the included update workflows with the
   same tag after the public verification passes.

If public verification finds a defect, preserve the released record, investigate
locally, and publish a corrected version only after the new candidate passes the
same gates.

## Homebrew and Scoop without a cross-repository token

The source repository's `GITHUB_TOKEN` cannot write to another repository.
Instead of adding a PAT, each package repository owns a small manual workflow:

- `rrrrrredy/homebrew-tap` uses
  [`homebrew/tap-update.yml`](homebrew/tap-update.yml) and stores the generated
  file at `Casks/beforedone.rb`.
- `rrrrrredy/scoop-bucket` uses
  [`scoop/bucket-update.yml`](scoop/bucket-update.yml) and stores the generated
  file at the repository root as `beforedone.json`.

Each workflow downloads the manifest from the public BeforeDone release and
commits it using that package repository's own built-in `GITHUB_TOKEN`. This
avoids a long-lived cross-repository credential. When Actions are unavailable,
download the same release manifest, verify its release-asset digest, and commit
it directly from an audited local checkout instead.

After those repositories exist, users install with:

```console
brew tap rrrrrredy/tap
brew install --cask beforedone
```

```console
scoop bucket add beforedone https://github.com/rrrrrredy/scoop-bucket
scoop install beforedone
```

## macOS zero-cost boundary

The project does not pay for an Apple Developer identity, so release archives
are not notarized. Do not add an automatic `xattr` quarantine bypass. The
source-built fallback remains:

```console
go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest
```

If Apple signing is added in the future, it must be a separately approved
release-hardening change and cannot become a prerequisite for the free build.
