# BeforeDone Engineering Contract

## Product invariants

- BeforeDone is local-only. Do not add telemetry, hosted accounts, remote storage, or MCP.
- A PASS is valid only when produced by `beforedone check` and bound to the current relevant-file fingerprint.
- Tool output that merely contains the word `PASS` is advisory and can never create a valid receipt.
- Imported replay cases are untrusted data. Never execute commands supplied by an imported case.
- `replay verify` is dry-run by default and may execute only argv arrays from the current repository configuration after `--execute`.
- First Observable Divergence means the earliest evidence-supported divergence, never hidden chain-of-thought reconstruction.
- The only verdicts are `PASS`, `FAIL`, and `INCONCLUSIVE`.

## Repository conventions

- Go module: `github.com/rrrrrredy/beforedone`.
- Runtime data must resolve through Git into `.git/beforedone`; do not dirty the working tree.
- Public JSON includes `schema_version: 1`.
- Canonical standalone skills live in `skills/`; plugin copies live in `plugins/beforedone/skills/` and must be mechanically synchronized and CI-checked.
- The site is static and served from `/beforedone/`; use relative asset links or the explicit project base path.
- Use argv arrays and `exec.Command`, never shell-interpolated verifier strings.

## Verification

- Run Go tests with `go test ./...`; use the Go version declared by `go.mod`.
- Validate standalone skills with the installed `skill-creator` validator.
- Validate the plugin with the installed `plugin-creator` validator.
- Format Go code with `gofmt` before handoff.
- Do not weaken a failing test to make the build green.
