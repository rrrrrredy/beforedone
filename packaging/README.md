# BeforeDone release and package-manager strategy

The canonical release is the GitHub Release created from a final `vMAJOR.MINOR.PATCH`
tag. It contains six archives, SHA-256 checksums, one SPDX 2.3 SBOM per archive,
and generated Homebrew/Scoop manifests. An Actions-built release also contains
GitHub/Sigstore provenance attestations. A locally built manual release must say
that it has no GitHub OIDC provenance rather than implying that it does.

## Supported release matrix

| OS | Architectures | Archive |
| --- | --- | --- |
| macOS | amd64, arm64 | `.tar.gz` |
| Linux | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

All binaries are statically compiled with `CGO_ENABLED=0`. Neither release mode
requires a paid BeforeDone dependency. The Actions mode uses the repository's
built-in `GITHUB_TOKEN`, but it still depends on GitHub Actions being enabled and
available for the publishing account.

## Actions release procedure

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

## Manual no-Actions release procedure

Use this mode when repository Actions are intentionally disabled or no runner
quota is available.

1. Confirm the release commit is on public `main`, versions agree, and Actions
   are disabled so publishing the tag cannot start a workflow accidentally.
2. Run the full Go suite, distribution validator, workflow lint, and a full
   Git-history secret scan locally. Record the exact tool versions and source
   commit used.
3. Build the six archives, checksums, six SPDX SBOMs, and package manifests with
   the pinned local GoReleaser and Syft versions.
4. Create a draft GitHub Release targeting the exact commit. GitHub may create a
   lightweight tag when the Release creates the tag; an annotated tag is not a
   requirement for this mode. Never rewrite an already published release tag
   merely to change its tag object type.
5. Upload every asset, download the complete draft into a new directory, compare
   all SHA-256 digests with the local build, and execute at least one downloaded
   native binary before publishing the draft.
6. Verify the public `releases/latest` route and an unauthenticated asset download.
   State explicitly that a manual release has no GitHub OIDC build provenance.

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
