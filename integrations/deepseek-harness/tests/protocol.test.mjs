import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  assertCompatibleCliVersion,
  createAgentStoppingEvent,
  createSessionEndedEvent,
  createSessionStartedEvent,
  failureSteeringText,
  gateSteeringText,
  parseGateResult,
  parseIngestResult,
  projectSessionEvent,
} from "../lib/protocol.js";

const integrationRoot = fileURLToPath(new URL("../", import.meta.url));

function session(id = "session-1") {
  return { id, seq: 9, header: { cwd: path.resolve("fixture-repo") } };
}

test("accepts the supported CLI range and rejects old, future-major, and malformed builds", () => {
  assert.equal(assertCompatibleCliVersion("beforedone 1.1.0\n").raw, "1.1.0");
  assert.equal(assertCompatibleCliVersion("beforedone v1.8.3\n").raw, "v1.8.3");
  assert.equal(assertCompatibleCliVersion("beforedone 1.1.0-dev\n").raw, "1.1.0-dev");
  assert.throws(() => assertCompatibleCliVersion("beforedone 1.0.2\n"), />=1\.1\.0 <2/);
  assert.throws(() => assertCompatibleCliVersion("beforedone 2.0.0\n"), />=1\.1\.0 <2/);
  assert.throws(() => assertCompatibleCliVersion("beforedone dev\n"), /semantic version/);
});

test("validates ingest acknowledgements without accepting partial batches", () => {
  const events = [{ id: "one" }, { id: "two" }];
  assert.deepEqual(
    parseIngestResult(
      JSON.stringify({ schema_version: 1, ingested: 2, event_ids: ["one", "two"] }),
      events,
    ).event_ids,
    ["one", "two"],
  );
  assert.throws(
    () =>
      parseIngestResult(
        JSON.stringify({ schema_version: 1, ingested: 1, event_ids: ["one"] }),
        events,
      ),
    /complete event batch/,
  );
  assert.throws(() => parseIngestResult("{broken", events), /malformed JSON/);
});

test("validates gate decisions and semantic consistency", () => {
  const pass = parseGateResult(
    JSON.stringify({ schema_version: 1, decision: "allow", verdict: "PASS", checks: [] }),
  );
  assert.equal(pass.decision, "allow");

  const blocked = parseGateResult(
    JSON.stringify({
      schema_version: 1,
      decision: "block",
      verdict: "INCONCLUSIVE",
      reason: "missing receipt",
      checks: [{ check_id: "unit", verdict: "INCONCLUSIVE" }],
    }),
  );
  assert.equal(blocked.reason, "missing receipt");

  assert.throws(
    () =>
      parseGateResult(
        JSON.stringify({ schema_version: 1, decision: "block", verdict: "PASS", reason: "x", checks: [] }),
      ),
    /inconsistent PASS/,
  );
  assert.throws(() => parseGateResult("not-json"), /malformed JSON/);
});

test("projects only lifecycle metadata and never copies prompt, arguments, or result content", () => {
  const current = session();
  const prompt = projectSessionEvent(
    current,
    {
      type: "user/message",
      seq: 3,
      time: 1000,
      data: {
        id: "message-1",
        role: "user",
        source: { kind: "user" },
        content: [{ type: "text", text: "PROMPT_SECRET" }],
      },
    },
    4,
  );
  const pluginPrompt = projectSessionEvent(
    current,
    {
      type: "user/message",
      seq: 6,
      time: 1500,
      data: {
        id: "message-2",
        role: "user",
        source: { kind: "plugin", plugin: "beforedone" },
        content: [{ type: "text", text: "PLUGIN_PROMPT_SECRET" }],
      },
    },
    4,
  );
  const started = projectSessionEvent(current, {
    type: "tool/call",
    seq: 4,
    time: 2000,
    data: { turn: 4, step: 1, callId: "call-1", name: "bash", arguments: "ARG_SECRET" },
  });
  const finished = projectSessionEvent(
    current,
    {
      type: "tool/result",
      seq: 5,
      time: 3000,
      data: {
        turn: 4,
        step: 1,
        message: {
          source: { kind: "tool", callId: "call-1" },
          content: [
            {
              type: "tool-result",
              toolCallId: "call-1",
              content: [{ type: "text", text: "RESULT_SECRET" }],
              isError: true,
            },
          ],
        },
      },
    },
    4,
    "bash",
  );

  const encoded = JSON.stringify([prompt, pluginPrompt, started, finished]);
  assert.doesNotMatch(encoded, /PROMPT_SECRET|PLUGIN_PROMPT_SECRET|ARG_SECRET|RESULT_SECRET/);
  assert.equal(prompt.type, "PromptSubmitted");
  assert.equal(pluginPrompt.attributes.message_plugin, "beforedone");
  assert.equal(started.tool_name, "bash");
  assert.equal(finished.tool_name, "bash");
  assert.equal(finished.exit_code, 1);
});

test("creates stable bounded lifecycle records and bounded steering", () => {
  const current = session("x".repeat(400));
  const lifecycleId = "lifecycle-1";
  const records = [
    createSessionStartedEvent(current, "resume", lifecycleId),
    createAgentStoppingEvent(current, 7, 1, lifecycleId),
    createSessionEndedEvent(current, lifecycleId),
  ];
  assert.ok(records.every((event) => event.id.length <= 256));
  assert.deepEqual(records.map((event) => event.type), ["SessionStarted", "AgentStopping", "SessionEnded"]);
  assert.match(gateSteeringText({ reason: "run unit" }), /beforedone check/);
  assert.match(failureSteeringText(new Error("timeout")), /timeout/);
  assert.ok(gateSteeringText({ reason: "x".repeat(20000) }).length < 12100);
  assert.notEqual(
    createSessionStartedEvent(current, "resume", "lifecycle-2").id,
    records[0].id,
    "a later resume of the same persisted session needs a distinct event ID",
  );
});

test("declares the installable Bundle and explicit bounded defaults", () => {
  const packageJson = JSON.parse(readFileSync(path.join(integrationRoot, "package.json"), "utf8"));
  assert.equal(packageJson.dsh?.bundle?.patch, "./cordis.patch.yml");
  assert.equal(packageJson.version, "0.1.0");
  assert.equal(packageJson.license, "Apache-2.0");
  assert.equal(
    packageJson.homepage,
    "https://github.com/rrrrrredy/beforedone#deepseek-harness-community-bundle",
  );
  assert.equal(packageJson.peerDependencies?.["@deepseek-ai/dsh-agent"], "0.1.0-rc.6");
  const patch = readFileSync(path.join(integrationRoot, "cordis.patch.yml"), "utf8").replaceAll("\r\n", "\n");
  assert.match(patch, /name: dsh-beforedone/);
  assert.match(patch, /gateTimeoutMs: 60000/);
  assert.match(patch, /stdoutMaxBytes: 1048576/);
  assert.match(patch, /stderrMaxBytes: 262144/);
});
