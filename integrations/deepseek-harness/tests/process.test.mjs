import assert from "node:assert/strict";
import test from "node:test";

import { runCli } from "../lib/process.js";

function reader(output, lossy = false) {
  return { readFrom: () => ({ text: output, nextOffset: Buffer.byteLength(output), lossy }) };
}

function fakeContext(handler) {
  return {
    subprocess: {
      spawn(spec) {
        const state = { stdout: "", stderr: "", stdoutLossy: false, stderrLossy: false };
        const done = Promise.resolve(handler(spec)).then((result) => {
          Object.assign(state, result);
          return { exitCode: result.exitCode ?? 0, signal: result.signal ?? null };
        });
        return {
          pid: 1,
          stdin: undefined,
          stdout: undefined,
          stderr: undefined,
          collected: {
            stdout: { readFrom: () => reader(state.stdout, state.stdoutLossy).readFrom() },
            stderr: { readFrom: () => reader(state.stderr, state.stderrLossy).readFrom() },
          },
          done,
          terminate() {},
          async waitForExit() { return true; },
        };
      },
    },
  };
}

const base = {
  argv: ["beforedone", "adapter", "ingest", "-", "--json"],
  cwd: process.cwd(),
  stdin: "[]\n",
  timeoutMs: 1000,
  stdoutMaxBytes: 1024,
  stderrMaxBytes: 1024,
  label: "test invocation",
};

test("passes an argv array and batch stdin without a shell field", async () => {
  let captured;
  const result = await runCli(
    fakeContext((spec) => {
      captured = spec;
      return { stdout: "ok", stderr: "", exitCode: 0 };
    }),
    base,
  );
  assert.deepEqual(captured.argv, base.argv);
  assert.deepEqual(captured.stdio.stdin, { data: "[]\n" });
  assert.equal("shell" in captured, false);
  assert.equal(result.stdout, "ok");
});
test("fails on bounded-output loss instead of parsing a partial tail", async () => {
  await assert.rejects(
    runCli(fakeContext(() => ({ stdout: "tail", stderr: "", stdoutLossy: true })), base),
    /stdout exceeded 1024 bytes/,
  );
  await assert.rejects(
    runCli(fakeContext(() => ({ stdout: "", stderr: "tail", stderrLossy: true })), base),
    /stderr exceeded 1024 bytes/,
  );
});

test("aborts and classifies a hung child as a timeout", async () => {
  const ctx = fakeContext(
    (spec) =>
      new Promise((resolve) => {
        spec.signal.addEventListener(
          "abort",
          () => resolve({ stdout: "", stderr: "", exitCode: null, signal: "SIGTERM" }),
          { once: true },
        );
      }),
  );
  await assert.rejects(runCli(ctx, { ...base, timeoutMs: 20 }), /timed out after 20ms/);
});
