# dsh-beforedone

Community DeepSeek Harness Bundle for [BeforeDone](https://github.com/rrrrrredy/beforedone). It projects Harness lifecycle metadata into BeforeDone's local event ledger and evaluates the native BeforeDone completion gate at `agent/turn-stopping`.

This is a community plugin, not an official DeepSeek plugin.

## Compatibility

- DeepSeek Harness / `@deepseek-ai/dsh`: `0.1.0-rc.6`
- BeforeDone CLI: `>=1.1.1 <2`
- Node.js: `22.19.x` or `24+`

The Bundle fails during activation when the CLI is missing, reports an unsupported version, or cannot be resolved in the Harness subprocess execution world.

## Install

Install and initialize the BeforeDone CLI in the target Git repository first:

```bash
go install github.com/rrrrrredy/beforedone/cmd/beforedone@v1.1.1
beforedone init
beforedone doctor
```

Then install the public Bundle into the Harness profile you use:

```bash
dsh plugin --profile headless add dsh-beforedone@0.1.0
dsh --profile headless --dump-config
```

For the browser surface, replace `headless` with `web`. For a local release candidate, replace the npm specifier with the absolute path to the packed `.tgz`.

The default configuration resolves `beforedone` from the subprocess provider's scrubbed `PATH`. To pin an executable, update the `beforedone` row in that profile's `cordis.patch.yml`:

```yaml
- id: beforedone
  config:
    binary: /absolute/path/to/beforedone
    gateTimeoutMs: 60000
    stdoutMaxBytes: 1048576
    stderrMaxBytes: 262144
```

Use an absolute Windows path when the executable is not on `PATH`. The Bundle always passes an argv array directly to `ctx.subprocess`; it never constructs a shell command.

## Use

Use Harness normally from inside a Git repository configured with `.beforedone.yaml`:

```bash
dsh --profile headless "Implement the change and verify it before finishing."
```

At the first completion boundary, the Bundle flushes normalized events and runs `beforedone gate --json` in the session working directory. A blocking or unsafe result becomes one durable plugin-originated steering message. The agent gets one corrective continuation in that turn. BeforeDone reevaluates the next stop, but the Bundle never forces the same turn more than once, so a broken check or CLI cannot create an infinite loop.

Only a current `PASS` receipt created by `beforedone check` for the configured relevant-file fingerprint can satisfy a required check. Ordinary log text containing `PASS` is not evidence.

## Captured events

The Bundle maps these Harness boundaries into BeforeDone's normalized Adapter contract:

| Harness boundary | BeforeDone event |
| --- | --- |
| `agent/session-start` | `SessionStarted` |
| `user/message` | `PromptSubmitted` |
| `tool/call` | `ToolStarted` |
| `tool/result` | `ToolFinished` |
| `agent/turn-stopping` | `AgentStopping` |
| `session/disposed` | `SessionEnded` |

Events are buffered per session and written through `beforedone adapter ingest - --json` at `session/flush` and completion boundaries. A failed, partial, truncated, timed-out, or malformed CLI response is an explicit capture gap and is treated as unsafe at completion.

## Permissions and privacy

- The Bundle and CLI run locally and add no telemetry, hosted service, remote storage, or MCP server.
- Normalized events contain the local session working directory, session/turn/step identifiers, lifecycle ID, event sequence, message source/plugin name, tool name, call ID, and success/error status.
- Prompt text, assistant reasoning, tool arguments, and tool-result content are never copied into the BeforeDone ledger.
- BeforeDone runtime data stays under the target repository's `.git/beforedone` directory. It is outside tracked worktree content and is not part of the npm package.
- Harness still sends ordinary conversation content to the model provider configured for that profile; this Bundle does not change that normal provider boundary.

BeforeDone is a cooperative evidence guardrail, not an OS security boundary or remote attestation system. A process with the same repository and operating-system privileges can alter the configuration or local evidence store.

## Uninstall

```bash
dsh plugin --profile headless remove dsh-beforedone
dsh --profile headless --dump-config
```

If you added a local `beforedone` override row to the profile's
`cordis.patch.yml`, remove that row too.

After removal, the Bundle row and installed package path are absent, no lifecycle listener remains, and later Harness sessions do not write BeforeDone events unless another BeforeDone integration is configured.

## Reproduce the tests

From this directory:

```bash
pnpm install --frozen-lockfile
pnpm verify
```

`pnpm verify` covers version/config validation, privacy projection, duplicate suppression, bounded stdout/stderr, timeouts, damaged JSON, real Cordis session events, bounded steering, and disposal.

Run the real local CLI combination test with explicit scratch paths:

```powershell
$env:BEFOREDONE_CLI = "D:\path\to\beforedone.exe"
$env:BEFOREDONE_TEST_ROOT = "D:\path\to\scratch"
pnpm test:integration
```

Run the complete tarball/public-package DSH test with an isolated profile:

```powershell
$env:DSH_ENTRY = "D:\path\to\@deepseek-ai\dsh\lib\bin.js"
$env:DSH_TARBALL = "D:\path\to\dsh-beforedone-0.1.0.tgz"
$env:BEFOREDONE_CLI = "D:\path\to\beforedone.exe"
$env:DSH_E2E_ROOT = "D:\path\to\scratch"
pnpm test:e2e:dsh
```

For the public registry, set `DSH_PACKAGE_SPEC=dsh-beforedone@0.1.0` and omit `DSH_TARBALL`. The E2E installs into a fresh profile, uses a loopback-only deterministic model endpoint, proves one blocked completion, runs a real `beforedone check`, verifies the second stop is allowed, inspects both append-only logs, removes the Bundle, and proves later sessions produce no BeforeDone side effect.
