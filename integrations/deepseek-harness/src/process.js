import {
  assertCompatibleCliVersion,
  parseGateResult,
  parseIngestResult,
} from "./protocol.js";

function abortScope(externalSignal, timeoutMs, label) {
  const controller = new AbortController();
  let timedOut = false;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort(new Error(`${label} exceeded ${timeoutMs}ms`));
  }, timeoutMs);

  const onAbort = () => controller.abort(externalSignal.reason);
  if (externalSignal?.aborted) onAbort();
  else externalSignal?.addEventListener("abort", onAbort, { once: true });

  return {
    signal: controller.signal,
    timedOut: () => timedOut,
    dispose() {
      clearTimeout(timeout);
      externalSignal?.removeEventListener("abort", onAbort);
    },
  };
}

function readCollected(handle, stream) {
  const reader = handle.collected[stream];
  if (!reader) throw new Error(`BeforeDone ${stream} was not collected`);
  return reader.readFrom(0);
}

export async function resolveCliBinary(ctx, binary, timeoutMs) {
  const scope = abortScope(undefined, timeoutMs, "BeforeDone executable lookup");
  try {
    return await ctx.subprocess.resolveExecutable(binary, undefined, scope.signal);
  } catch (error) {
    if (scope.timedOut()) {
      throw new Error(`BeforeDone executable lookup timed out after ${timeoutMs}ms`, { cause: error });
    }
    throw new Error(`Cannot resolve BeforeDone executable ${JSON.stringify(binary)}`, { cause: error });
  } finally {
    scope.dispose();
  }
}

export async function runCli(
  ctx,
  {
    argv,
    cwd,
    stdin,
    timeoutMs,
    stdoutMaxBytes,
    stderrMaxBytes,
    signal,
    label,
  },
) {
  const scope = abortScope(signal, timeoutMs, label);
  let handle;
  let outcome;
  try {
    handle = ctx.subprocess.spawn({
      argv,
      cwd,
      stdio: {
        stdin: stdin === undefined ? "ignore" : { data: stdin },
        stdout: { maxBytes: stdoutMaxBytes },
        stderr: { maxBytes: stderrMaxBytes },
      },
      graceMs: Math.max(1, Math.min(1000, timeoutMs)),
      signal: scope.signal,
    });
    outcome = await handle.done;
  } catch (error) {
    signal?.throwIfAborted();
    if (scope.timedOut()) {
      throw new Error(`${label} timed out after ${timeoutMs}ms`, { cause: error });
    }
    throw new Error(`${label} could not run`, { cause: error });
  } finally {
    scope.dispose();
  }

  signal?.throwIfAborted();
  if (scope.timedOut()) throw new Error(`${label} timed out after ${timeoutMs}ms`);

  const stdout = readCollected(handle, "stdout");
  const stderr = readCollected(handle, "stderr");
  if (stdout.lossy) throw new Error(`${label} stdout exceeded ${stdoutMaxBytes} bytes`);
  if (stderr.lossy) throw new Error(`${label} stderr exceeded ${stderrMaxBytes} bytes`);
  return {
    exitCode: outcome.exitCode,
    signal: outcome.signal,
    stdout: stdout.text,
    stderr: stderr.text,
  };
}

function requireExit(result, allowed, label) {
  if (!allowed.includes(result.exitCode) || result.signal !== null) {
    const detail = result.stderr.trim();
    throw new Error(
      `${label} exited unexpectedly (code ${String(result.exitCode)}, signal ${String(result.signal)})${detail ? `: ${detail}` : ""}`,
    );
  }
}

function commonOptions(runtime, cwd, signal, label) {
  return {
    cwd,
    timeoutMs: runtime.config.gateTimeoutMs,
    stdoutMaxBytes: runtime.config.stdoutMaxBytes,
    stderrMaxBytes: runtime.config.stderrMaxBytes,
    signal,
    label,
  };
}

export async function inspectCli(runtime, cwd) {
  const result = await runCli(runtime.ctx, {
    ...commonOptions(runtime, cwd, undefined, "BeforeDone version check"),
    argv: [runtime.binary, "--version"],
  });
  requireExit(result, [0], "BeforeDone version check");
  return assertCompatibleCliVersion(result.stdout);
}

export async function ingestEvents(runtime, cwd, events, signal) {
  const result = await runCli(runtime.ctx, {
    ...commonOptions(runtime, cwd, signal, "BeforeDone adapter ingest"),
    argv: [runtime.binary, "adapter", "ingest", "-", "--json"],
    stdin: `${JSON.stringify(events)}\n`,
  });
  requireExit(result, [0], "BeforeDone adapter ingest");
  return parseIngestResult(result.stdout, events);
}

export async function evaluateGate(runtime, cwd, signal) {
  const result = await runCli(runtime.ctx, {
    ...commonOptions(runtime, cwd, signal, "BeforeDone gate"),
    argv: [runtime.binary, "gate", "--json"],
  });
  requireExit(result, [0, 1, 2], "BeforeDone gate");
  const gate = parseGateResult(result.stdout);
  const expectedExit = { PASS: 0, FAIL: 1, INCONCLUSIVE: 2 }[gate.verdict];
  if (result.exitCode !== expectedExit) {
    throw new Error(
      `BeforeDone gate exit code ${String(result.exitCode)} did not match verdict ${gate.verdict}`,
    );
  }
  return gate;
}
