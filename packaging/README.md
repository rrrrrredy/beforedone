# BeforeDone release and package-manager strategy

The canonical release is the GitHub Release created from a final `vMAJOR.MINOR.PATCH`
tag. It contains six archives, SHA-256 checksums, one SPDX 2.3 SBOM per archive,
and generated Homebrew/Scoop manifests. The workflow also creates GitHub/Sigstore
provenance attestations for the downloadable artifacts.

## Supported release matrix

| OS | Architectures | Archive |
| --- | --- | --- |
| macOS | amd64, arm64 | `.tar.gz` |
| Linux | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

All binaries are statically compiled with `CGO_ENABLED=0`. The release workflow
uses only the repository's built-in `GITHUB_TOKEN`; Syft, GoReleaser, GitHub
Actions, GitHub Releases, and artifact attestations are free for this public
repository.

## Release procedure

1. Confirm the `CI`, `Security`, `Dependency Review`, and Pages checks are green
   on `main`.
2. Confirm the plugin manifest, standalone skills, and CLI all report the same
   version that will be tagged.
3. Create and push an annotated final SemVer tag, for example `v1.0.0`.
4. Watch the `Release` workflow. GoReleaser first creates a draft; three fresh
   runners download, checksum, extract, and execute the public-format artifacts.
   Only then does the workflow make the release public. Do not create or edit
   assets by hand while it is running.
5. Verify the published release from a clean Windows, macOS, and Linux machine.
6. In the two package repositories, manually run the included update workflows
   with the same tag.

The release job rejects prerelease-shaped tags and tags whose commit is not
reachable from `main`.

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
keeps the pipeline free and avoids a long-lived cross-repository credential.

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
