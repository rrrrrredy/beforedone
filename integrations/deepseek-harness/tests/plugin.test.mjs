import assert from "node:assert/strict";
import test from "node:test";

import { Context } from "@deepseek-ai/cordis";
import { createToolResultMessage, createUserMessage } from "@deepseek-ai/dsh-llm";
import SessionStore, { SessionId } from "@deepseek-ai/dsh-session";
import { SubprocessRuntime } from "@deepseek-ai/dsh-subprocess";

import * as BeforeDoneBundle from "../lib/index.js";

function collected(state, key) {
  return {
    readFrom() {
      const text = state[key] || "";
      return { text, nextOffset: Buffer.byteLength(text), lossy: Boolean(state[`${key}Lossy`]) };
    },
  };
}

function scriptedSubprocess(handler, calls) {
  return class ScriptedSubprocess extends SubprocessRuntime {
    constructor(ctx) {
      super(ctx);
    }

    async resolveExecutable(command) {
      if (command === "missing") throw new Error("not found");
      return command;
    }

    spawn(spec) {
      calls.push(spec);
      const state = {};
      const done = Promise.resolve(handler(spec, calls)).then((result) => {
        Object.assign(state, result);
        return { exitCode: result.exitCode ?? 0, signal: result.signal ?? null };
      });
      return {
        pid: calls.length,
        stdin: undefined,
        stdout: undefined,
        stderr: undefined,
        collected: { stdout: collected(state, "stdout"), stderr: collected(state, "stderr") },
        done,
        terminate() {},
        async waitForExit() { return true; },
      };
    }

    async spawnTerminal() {
      throw new Error("not implemented");
    }
  };
}

function responseFor(spec, gateResult) {
  if (spec.argv[1] === "--version") {
    return { stdout: "beforedone 1.1.1\n", stderr: "", exitCode: 0 };
  }
  if (spec.argv[1] === "adapter") {
    const events = JSON.parse(spec.stdio.stdin.data);
    return {
      stdout: JSON.stringify({
        schema_version: 1,
        ingested: events.length,
        event_ids: events.map((event) => event.id),
      }),
      stderr: "",
      exitCode: 0,
    };
  }
  if (spec.argv[1] === "gate") {
    return {
      stdout: JSON.stringify(gateResult),
      stderr: "",
      exitCode: gateResult.verdict === "PASS" ? 0 : gateResult.verdict === "FAIL" ? 1 : 2,
    };
  }
  throw new Error(`unexpected argv ${JSON.stringify(spec.argv)}`);
}

async function createHarness(handler) {
  const calls = [];
  const ctx = new Context();
  const sessionsFiber = ctx.plugin(SessionStore);
  await sessionsFiber.await();
  const subprocessFiber = ctx.plugin(scriptedSubprocess(handler, calls));
  await subprocessFiber.await();
  const bundleFiber = ctx.plugin(BeforeDoneBundle, {
    binary: "beforedone",
    gateTimeoutMs: 1000,
    stdoutMaxBytes: 4096,
    stderrMaxBytes: 4096,
  });
  await bundleFiber.await();
  return { ctx, calls, sessionsFiber, subprocessFiber, bundleFiber };
}

test("captures real SessionStore events, gates once per bounded retry, and unregisters cleanly", async () => {
  const blocked = {
    schema_version: 1,
    decision: "block",
    verdict: "INCONCLUSIVE",
    reason: "missing fresh evidence",
    checks: [{ check_id: "unit", verdict: "INCONCLUSIVE" }],
  };
  const harness = await createHarness((spec) => responseFor(spec, blocked));
  const { ctx, calls, bundleFiber, subprocessFiber, sessionsFiber } = harness;
  let session;
  const owner = ctx.plugin({
    name: "session-owner",
    inject: ["sessions"],
    apply(ownerCtx) {
      session = ownerCtx.sessions.create(SessionId("bundle-test"), { meta: { cwd: process.cwd() } });
    },
  });
  await owner.await();
  const steered = [];
  const agent = { session, steer: (message) => steered.push(message) };
  ctx.emit("agent/session-start", { agent, source: "startup" });
  session.append("turn/start", { turn: 1 });
  session.append(
    "user/message",
    createUserMessage({ content: [{ type: "text", text: "private prompt" }], source: { kind: "user" } }),
    { surfaceOp: "append" },
  );
  session.append("tool/call", {
    turn: 1,
    step: 1,
    callId: "call-1",
    name: "bash",
    arguments: '{"secret":"value"}',
  });
  session.append(
    "tool/result",
    {
      turn: 1,
      step: 1,
      message: createToolResultMessage({
        callId: "call-1",
        content: [{ type: "text", text: "private result" }],
        isError: false,
      }),
    },
    { surfaceOp: "append" },
  );
  await ctx.sessions.flush(session);

  const controller = new AbortController();
  await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal: controller.signal });
  assert.equal(steered.length, 1);
  assert.match(steered[0].content[0].text, /missing fresh evidence/);
  await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal: controller.signal });
  assert.equal(steered.length, 1, "the same turn must never be forced more than once");

  const ingested = calls
    .filter((spec) => spec.argv[1] === "adapter")
    .flatMap((spec) => JSON.parse(spec.stdio.stdin.data));
  assert.deepEqual(
    new Set(ingested.map((event) => event.type)),
    new Set(["SessionStarted", "PromptSubmitted", "ToolStarted", "ToolFinished", "AgentStopping"]),
  );
  assert.doesNotMatch(JSON.stringify(ingested), /private prompt|private result|\"secret\"/);
  assert.equal(ingested.filter((event) => event.type === "AgentStopping").length, 2);

  await owner.dispose();
  await bundleFiber.dispose();
  assert.ok(
    calls
      .filter((spec) => spec.argv[1] === "adapter")
      .flatMap((spec) => JSON.parse(spec.stdio.stdin.data))
      .some((event) => event.type === "SessionEnded"),
  );

  const callsAfterRemoval = calls.length;
  session.append("turn/start", { turn: 2 });
  assert.equal(calls.length, callsAfterRemoval);
  await subprocessFiber.dispose();
  await sessionsFiber.dispose();
});

test("uses a distinct lifecycle ID when the same persisted session ID resumes", async () => {
  const allowed = {
    schema_version: 1,
    decision: "allow",
    verdict: "PASS",
    checks: [],
  };
  const { ctx, calls, bundleFiber, subprocessFiber, sessionsFiber } =
    await createHarness((spec) => responseFor(spec, allowed));

  async function runLifecycle(ownerName) {
    let session;
    const owner = ctx.plugin({
      name: ownerName,
      inject: ["sessions"],
      apply(ownerCtx) {
        session = ownerCtx.sessions.create(SessionId("persisted-session"), {
          meta: { cwd: process.cwd() },
        });
      },
    });
    await owner.await();
    ctx.emit("agent/session-start", {
      agent: { session, steer() {} },
      source: "resume",
    });
    await ctx.sessions.flush(session);
    await owner.dispose();
  }

  await runLifecycle("first-session-owner");
  await runLifecycle("second-session-owner");
  await bundleFiber.dispose();

  const starts = calls
    .filter((spec) => spec.argv[1] === "adapter")
    .flatMap((spec) => JSON.parse(spec.stdio.stdin.data))
    .filter((event) => event.type === "SessionStarted");
  assert.equal(starts.length, 2);
  assert.notEqual(starts[0].id, starts[1].id);
  assert.notEqual(starts[0].attributes.lifecycle_id, starts[1].attributes.lifecycle_id);

  await subprocessFiber.dispose();
  await sessionsFiber.dispose();
});

test("fails activation for a missing or incompatible CLI", async () => {
  {
    const ctx = new Context();
    const sessionsFiber = ctx.plugin(SessionStore);
    await sessionsFiber.await();
    const subprocessFiber = ctx.plugin(scriptedSubprocess(() => ({}), []));
    await subprocessFiber.await();
    const failedFiber = ctx.plugin(BeforeDoneBundle, {
      binary: "missing",
      gateTimeoutMs: 1000,
      stdoutMaxBytes: 4096,
      stderrMaxBytes: 4096,
    });
    await assert.rejects(
      failedFiber.await(),
      /Cannot resolve BeforeDone executable/,
    );
    await failedFiber.dispose();
    await subprocessFiber.dispose();
    await sessionsFiber.dispose();
  }

  {
    const ctx = new Context();
    const sessionsFiber = ctx.plugin(SessionStore);
    await sessionsFiber.await();
    const subprocessFiber = ctx.plugin(
      scriptedSubprocess(() => ({ stdout: "beforedone 1.0.2\n", stderr: "", exitCode: 0 }), []),
    );
    await subprocessFiber.await();
    const failedFiber = ctx.plugin(BeforeDoneBundle, {
      binary: "beforedone",
      gateTimeoutMs: 1000,
      stdoutMaxBytes: 4096,
      stderrMaxBytes: 4096,
    });
    await assert.rejects(
      failedFiber.await(),
      />=1\.1\.1 <2/,
    );
    await failedFiber.dispose();
    await subprocessFiber.dispose();
    await sessionsFiber.dispose();
  }
});

test("treats malformed gate JSON as unsafe and steers once", async () => {
  const harness = await createHarness((spec) => {
    if (spec.argv[1] === "gate") return { stdout: "{broken", stderr: "", exitCode: 2 };
    return responseFor(spec, {
      schema_version: 1,
      decision: "allow",
      verdict: "PASS",
      checks: [],
    });
  });
  const { ctx, bundleFiber, subprocessFiber, sessionsFiber } = harness;
  const session = ctx.sessions.create(SessionId("bad-json"), { meta: { cwd: process.cwd() } });
  const steered = [];
  const agent = { session, steer: (message) => steered.push(message) };
  const signal = new AbortController().signal;
  await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal });
  await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal });
  assert.equal(steered.length, 1);
  assert.match(steered[0].content[0].text, /malformed JSON/);
  await bundleFiber.dispose();
  await subprocessFiber.dispose();
  await sessionsFiber.dispose();
});
