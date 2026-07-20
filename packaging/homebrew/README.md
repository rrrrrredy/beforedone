# Homebrew tap bootstrap

1. Create the public repository `rrrrrredy/homebrew-tap` with a `main` branch.
2. Copy `tap-update.yml` to `.github/workflows/update.yml` in that repository.
3. In the repository's Actions settings, allow `GITHUB_TOKEN` read/write access.
4. Run **Update BeforeDone cask** and enter a published tag such as `v1.0.0`.
5. Confirm `Casks/beforedone.rb` was committed, then test on a clean macOS host:

```console
brew tap rrrrrredy/tap
brew audit --cask --online beforedone
brew install --cask beforedone
beforedone doctor
brew uninstall --cask beforedone
```

The update workflow never builds a binary and never accepts an arbitrary URL.
It downloads only `beforedone.rb` from the fixed public source repository.
