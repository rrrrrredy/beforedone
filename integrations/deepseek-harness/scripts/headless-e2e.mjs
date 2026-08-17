import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { createServer } from "node:http";
import { access, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";

const PACKAGE_NAME = "dsh-beforedone";
const TASK_TEXT = "Finish this repository task only after its required evidence is fresh.";
const FINAL_TEXT = "DETERMINISTIC_DSH_BEFOREDONE_OK";
const AFTER_REMOVE_TEXT = "DETERMINISTIC_DSH_BEFOREDONE_REMOVED";

function requiredPath(name) {
  const value = process.env[name];
  if (!value?.trim() || !path.isAbsolute(value)) {
    throw new Error(`${name} must be an absolute path`);
  }
  return path.normalize(value);
}

const dshEntry = requiredPath("DSH_ENTRY");
const beforedoneCli = requiredPath("BEFOREDONE_CLI");
const scratchRoot = requiredPath("DSH_E2E_ROOT");
const packageSpec = process.env.DSH_PACKAGE_SPEC?.trim();
const tarball = process.env.DSH_TARBALL?.trim();
if (Boolean(packageSpec) === Boolean(tarball)) {
  throw new Error("exactly one of DSH_PACKAGE_SPEC or DSH_TARBALL must be set");
}
const installSpec = packageSpec || requiredPath("DSH_TARBALL");
const keepArtifacts = process.env.DSH_E2E_KEEP === "1";

await Promise.all([
  access(dshEntry),
  access(beforedoneCli),
  ...(packageSpec ? [] : [access(installSpec)]),
]);
await mkdir(scratchRoot, { recursive: true });
const root = await mkdtemp(path.join(scratchRoot, "dsh-beforedone-"));
const home = path.join(root, "home");
const repository = path.join(root, "repository");
await Promise.all([mkdir(home), mkdir(repository)]);

function run(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, encoding: "utf8", windowsHide: true });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  return result.stdout;
}

function runDsh(args, extraEnv = {}, timeoutMs = 60_000) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [dshEntry, ...args], {
      cwd: repository,
      env: {
        ...process.env,
        DSH_HOME: home,
        DSH_TELEMETRY_MODE: "DISABLED",
        ...extraEnv,
      },
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => { stdout += chunk; });
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.once("error", reject);
    const timer = setTimeout(() => {
      child.kill();
      reject(new Error(`dsh timed out after ${timeoutMs}ms: ${args.join(" ")}\n${stderr}`));
    }, timeoutMs);
    child.once("close", (code, signal) => {
      clearTimeout(timer);
      resolve({ code: code ?? -1, signal, stdout, stderr });
    });
  });
}

function sse(response, payload) {
  response.write(`data: ${typeof payload === "string" ? payload : JSON.stringify(payload)}\n\n`);
}

function beginStream(response) {
  response.writeHead(200, {
    "content-type": "text/event-stream; charset=utf-8",
    "cache-control": "no-cache",
    connection: "keep-alive",
  });
}

function finishText(response, text) {
  beginStream(response);
  sse(response, { choices: [{ index: 0, delta: { content: text }, finish_reason: null }] });
  sse(response, {
    choices: [{ index: 0, delta: { content: "" }, finish_reason: "stop" }],
    usage: { prompt_tokens: 3, completion_tokens: text.length },
  });
  sse(response, "[DONE]");
  response.end();
}

function finishToolCall(response, id, name, args) {
  beginStream(response);
  sse(response, {
    choices: [
      {
        index: 0,
        delta: {
          tool_calls: [
            { index: 0, id, type: "function", function: { name, arguments: JSON.stringify(args) } },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  sse(response, {
    choices: [{ index: 0, delta: { content: "" }, finish_reason: "tool_calls" }],
    usage: { prompt_tokens: 3, completion_tokens: 2 },
  });
  sse(response, "[DONE]");
  response.end();
}

async function readBody(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(Buffer.from(chunk));
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

async function filesUnder(rootPath) {
  const files = [];
  async function walk(directory) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const candidate = path.join(directory, entry.name);
      if (entry.isDirectory()) await walk(candidate);
      else if (entry.isFile()) files.push(candidate);
    }
  }
  await walk(rootPath);
  return files;
}

function allStrings(value, output = []) {
  if (typeof value === "string") output.push(value);
  else if (Array.isArray(value)) for (const item of value) allStrings(item, output);
  else if (value && typeof value === "object") {
    for (const item of Object.values(value)) allStrings(item, output);
  }
  return output;
}

function quoteShell(value) {
  if (process.platform === "win32") return `'${value.replaceAll("'", "''")}'`;
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

async function readBeforeDoneEvents() {
  const segments = path.join(repository, ".git", "beforedone", "events", "segments");
  const files = (await readdir(segments)).filter((name) => name.endsWith(".jsonl")).sort();
  const events = [];
  for (const file of files) {
    const text = await readFile(path.join(segments, file), "utf8");
    for (const line of text.split(/\r?\n/).filter(Boolean)) events.push(JSON.parse(line));
  }
  return events;
}

function profilePatch() {
  const rows = [
    "- id: session-title-llm",
    "  disabled: true",
    "- id: session-persistence-jsonl",
    "  config:",
    "    root: !!js dshHomePath('sessions')",
    "    compression: none",
  ];
  rows.push("");
  return rows.join("\n");
}

let server;
let passed = false;
const requestBodies = [];
let phase = "installed";
try {
  run("git", ["init", "-q"], repository);
  run("git", ["config", "user.email", "test@example.invalid"], repository);
  run("git", ["config", "user.name", "BeforeDone Test"], repository);
  await writeFile(path.join(repository, "README.md"), "deterministic fixture\n", "utf8");
  await writeFile(
    path.join(repository, ".beforedone.yaml"),
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
  run("git", ["add", "."], repository);
  run("git", ["commit", "-m", "fixture"], repository);

  const install = await runDsh(["plugin", "--profile", "headless", "add", installSpec]);
  assert.equal(install.code, 0, install.stderr || install.stdout);
  const profileDir = path.join(home, "profiles", "headless");
  const installedRoot = path.join(profileDir, "node_modules", PACKAGE_NAME);
  await access(installedRoot);
  await writeFile(path.join(profileDir, "cordis.patch.yml"), profilePatch(), "utf8");

  const dump = await runDsh(["--profile", "headless", "--dump-config"]);
  assert.equal(dump.code, 0, dump.stderr || dump.stdout);
  const normalizedDump = `${dump.stdout}\n${dump.stderr}`.replace(/[\\/]+/g, "/");
  assert.ok(normalizedDump.includes(`# == ${PACKAGE_NAME}`));
  assert.ok(normalizedDump.includes(`name: ${PACKAGE_NAME}`));
  assert.ok(normalizedDump.includes("binary: beforedone"));
  assert.ok(normalizedDump.includes("gateTimeoutMs: 60000"));

  const shellTool = process.platform === "win32" ? "pwsh" : "bash";
  const checkCommand = `${process.platform === "win32" ? "& " : ""}${quoteShell(beforedoneCli)} check unit --json`;
  server = createServer(async (request, response) => {
    try {
      if (request.method !== "POST" || !request.url?.endsWith("/chat/completions")) {
        response.writeHead(404).end();
        return;
      }
      if (request.headers.authorization !== "Bearer deterministic-mock-key") {
        response.writeHead(401, { "content-type": "application/json" });
        response.end(JSON.stringify({ error: { message: "bad mock key" } }));
        return;
      }
      const body = await readBody(request);
      requestBodies.push(body);
      if (phase === "removed") {
        finishText(response, AFTER_REMOVE_TEXT);
      } else if (requestBodies.length === 1) {
        finishText(response, "The requested work is complete.");
      } else if (requestBodies.length === 2) {
        finishToolCall(response, "call-check-1", shellTool, {
          command: checkCommand,
          description: "Generate fresh BeforeDone evidence",
          timeoutMs: 30_000,
        });
      } else if (requestBodies.length === 3) {
        finishText(response, FINAL_TEXT);
      } else {
        response.writeHead(500, { "content-type": "application/json" });
        response.end(JSON.stringify({ error: { message: "mock script exhausted" } }));
      }
    } catch (error) {
      response.writeHead(500, { "content-type": "application/json" });
      response.end(JSON.stringify({ error: { message: String(error) } }));
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  const pathKey = Object.keys(process.env).find((key) => key.toLowerCase() === "path") || "PATH";
  const modelEnv = {
    DEEPSEEK_API_KEY: "deterministic-mock-key",
    DEEPSEEK_BASE_URL: `http://127.0.0.1:${address.port}`,
    DSH_PERMISSION_MODE: "danger-full-access",
    NO_PROXY: "127.0.0.1,localhost",
    [pathKey]: [path.dirname(beforedoneCli), process.env[pathKey]].filter(Boolean).join(path.delimiter),
  };

  const runResult = await runDsh(["--profile", "headless", TASK_TEXT], modelEnv);
  assert.equal(runResult.code, 0, runResult.stderr || runResult.stdout);
  assert.equal(runResult.stdout.trim(), FINAL_TEXT);
  assert.equal(requestBodies.length, 3);
  const secondRequestText = allStrings(requestBodies[1]).join("\n");
  assert.match(secondRequestText, /BeforeDone cannot safely accept completion yet/);
  const thirdRequestText = allStrings(requestBodies[2]).join("\n");
  assert.match(thirdRequestText, /"verdict":\s*"PASS"/);

  const events = await readBeforeDoneEvents();
  const eventTypes = new Set(events.map((event) => event.type));
  for (const type of ["SessionStarted", "PromptSubmitted", "ToolStarted", "ToolFinished", "AgentStopping"]) {
    assert.ok(eventTypes.has(type), `missing normalized event type: ${type}`);
  }
  // DSH rc.6's one-shot headless runner flushes its session but does not dispose
  // the AgentHandle. SessionEnded is therefore exercised by integration.test.mjs,
  // where the real SessionStore lifecycle explicitly disposes the handle.
  assert.equal(events.filter((event) => event.type === "AgentStopping").length, 2);
  assert.equal(
    events.filter(
      (event) =>
        event.type === "PromptSubmitted" &&
        event.attributes?.message_source === "plugin" &&
        event.attributes?.message_plugin === "beforedone",
    ).length,
    1,
  );
  assert.doesNotMatch(JSON.stringify(events), /Finish this repository task|Generate fresh BeforeDone evidence/);

  const sessionFiles = (await filesUnder(path.join(home, "sessions"))).filter((file) => file.endsWith(".jsonl"));
  assert.ok(sessionFiles.length > 0);
  const sessionText = (await Promise.all(sessionFiles.map((file) => readFile(file, "utf8")))).join("\n");
  assert.match(sessionText, /BeforeDone cannot safely accept completion yet/);
  assert.match(sessionText, /"plugin":"beforedone"/);

  const beforeRemoveCount = events.length;
  const remove = await runDsh(["plugin", "--profile", "headless", "remove", PACKAGE_NAME]);
  assert.equal(remove.code, 0, remove.stderr || remove.stdout);
  const afterRemove = await runDsh(["--profile", "headless", "--dump-config"]);
  assert.equal(afterRemove.code, 0, afterRemove.stderr || afterRemove.stdout);
  const normalizedAfterRemove = `${afterRemove.stdout}\n${afterRemove.stderr}`.replace(/[\\/]+/g, "/");
  assert.ok(!normalizedAfterRemove.includes(`# == ${PACKAGE_NAME}`));
  assert.ok(!normalizedAfterRemove.includes(`name: ${PACKAGE_NAME}`));
  await assert.rejects(access(installedRoot), { code: "ENOENT" });

  phase = "removed";
  const runAfterRemove = await runDsh(["--profile", "headless", "Complete without plugins."], modelEnv);
  assert.equal(runAfterRemove.code, 0, runAfterRemove.stderr || runAfterRemove.stdout);
  assert.equal(runAfterRemove.stdout.trim(), AFTER_REMOVE_TEXT);
  assert.equal((await readBeforeDoneEvents()).length, beforeRemoveCount);

  passed = true;
  console.log(
    JSON.stringify({
      status: "passed",
      modelRequestsWithPlugin: 3,
      boundedSteering: true,
      freshEvidenceAllowed: true,
      normalizedEventTypes: [...new Set(events.map((event) => event.type))].sort(),
      removed: true,
      sideEffectsAfterRemoval: false,
      artifactsKept: keepArtifacts,
      ...(keepArtifacts ? { artifactRoot: root } : {}),
    }),
  );
} finally {
  if (server) await new Promise((resolve) => server.close(resolve));
  if (passed && !keepArtifacts) {
    if (path.dirname(root) !== path.resolve(scratchRoot) || !path.basename(root).startsWith("dsh-beforedone-")) {
      throw new Error(`refusing to remove unexpected E2E directory: ${root}`);
    }
    await rm(root, { recursive: true, force: true });
  } else if (!passed) {
    console.error(`Deterministic E2E artifacts retained at ${root}`);
  }
}
