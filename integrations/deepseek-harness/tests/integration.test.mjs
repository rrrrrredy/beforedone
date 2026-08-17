import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { Context } from "@deepseek-ai/cordis";
import { createToolResultMessage, createUserMessage } from "@deepseek-ai/dsh-llm";
import SessionStore, { SessionId } from "@deepseek-ai/dsh-session";
import LocalSubprocessRuntime from "@deepseek-ai/dsh-subprocess-local";

import * as BeforeDoneBundle from "../lib/index.js";

const cli = path.resolve(process.env.BEFOREDONE_CLI);
const scratchRoot = path.resolve(process.env.BEFOREDONE_TEST_ROOT);

function run(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, encoding: "utf8", windowsHide: true });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  return result.stdout;
}

async function readLedger(repo) {
  const segmentRoot = path.join(repo, ".git", "beforedone", "events", "segments");
  const files = (await readdir(segmentRoot)).filter((name) => name.endsWith(".jsonl")).sort();
  const events = [];
  for (const file of files) {
    const text = await readFile(path.join(segmentRoot, file), "utf8");
    for (const line of text.trim().split(/\r?\n/)) {
      if (line) events.push(JSON.parse(line));
    }
  }
  return events;
}

test("uses real Cordis SessionStore and local subprocess against the current BeforeDone CLI", async () => {
  await mkdir(scratchRoot, { recursive: true });
  const repo = await mkdtemp(path.join(scratchRoot, "dsh-beforedone-"));
  try {
    run("git", ["init", "-q"], repo);
    run("git", ["config", "user.email", "test@example.invalid"], repo);
    run("git", ["config", "user.name", "BeforeDone Test"], repo);
    await writeFile(path.join(repo, "README.md"), "fixture\n", "utf8");
    await writeFile(
      path.join(repo, ".beforedone.yaml"),
      [
        "schema_version: 1",
        "checks:",
        "  unit:",
        "    argv: [git, status, --short]",
        "    relevant_files: [README.md]",
        "    working_directory: .",
        "    timeout_seconds: 30",
        "",
      ].join("\n"),
      "utf8",
    );
    run("git", ["add", "."], repo);
    run("git", ["commit", "-m", "fixture"], repo);

    const ctx = new Context();
    const sessionsFiber = ctx.plugin(SessionStore);
    await sessionsFiber.await();
    const subprocessFiber = ctx.plugin(LocalSubprocessRuntime);
    await subprocessFiber.await();
    const bundleFiber = ctx.plugin(BeforeDoneBundle, {
      binary: cli,
      gateTimeoutMs: 30000,
      stdoutMaxBytes: 1048576,
      stderrMaxBytes: 262144,
    });
    await bundleFiber.await();

    let session;
    const owner = ctx.plugin({
      name: "integration-session-owner",
      inject: ["sessions"],
      apply(ownerCtx) {
        session = ownerCtx.sessions.create(SessionId("real-integration"), { meta: { cwd: repo } });
      },
    });
    await owner.await();
    const steered = [];
    const agent = { session, steer: (message) => steered.push(message) };
    ctx.emit("agent/session-start", { agent, source: "startup" });
    session.append("turn/start", { turn: 1 });
    session.append(
      "user/message",
      createUserMessage({ content: [{ type: "text", text: "do not persist this prompt" }], source: { kind: "user" } }),
      { surfaceOp: "append" },
    );
    session.append("tool/call", {
      turn: 1,
      step: 1,
      callId: "call-real",
      name: "bash",
      arguments: '{"private":"argument"}',
    });
    session.append(
      "tool/result",
      {
        turn: 1,
        step: 1,
        message: createToolResultMessage({
          callId: "call-real",
          content: [{ type: "text", text: "do not persist this result" }],
          isError: false,
        }),
      },
      { surfaceOp: "append" },
    );
    await ctx.sessions.flush(session);

    const signal = new AbortController().signal;
    await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal });
    assert.equal(steered.length, 1, "missing evidence must request one corrective continuation");

    run(cli, ["check", "unit", "--json"], repo);
    await ctx.serial("agent/turn-stopping", { agent, turn: 1, signal });
    assert.equal(steered.length, 1, "fresh PASS must not add another steering message");

    await owner.dispose();
    await bundleFiber.dispose();
    const events = await readLedger(repo);
    assert.deepEqual(
      new Set(events.map((event) => event.type)),
      new Set(["SessionStarted", "PromptSubmitted", "ToolStarted", "ToolFinished", "AgentStopping", "SessionEnded"]),
    );
    assert.equal(events.filter((event) => event.type === "AgentStopping").length, 2);
    assert.doesNotMatch(
      JSON.stringify(events),
      /do not persist this prompt|do not persist this result|\"private\"/,
    );

    await subprocessFiber.dispose();
    await sessionsFiber.dispose();
  } finally {
    await rm(repo, { recursive: true, force: true });
  }
});
