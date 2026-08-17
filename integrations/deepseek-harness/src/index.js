import { randomUUID } from "node:crypto";

import { createUserMessage } from "@deepseek-ai/dsh-llm";
import z from "@deepseek-ai/schemastery";

import { evaluateGate, ingestEvents, inspectCli, resolveCliBinary } from "./process.js";
import {
  createAgentStoppingEvent,
  createSessionEndedEvent,
  createSessionStartedEvent,
  failureSteeringText,
  gateSteeringText,
  projectSessionEvent,
} from "./protocol.js";

const DEFAULTS = Object.freeze({
  binary: "beforedone",
  gateTimeoutMs: 60000,
  stdoutMaxBytes: 1048576,
  stderrMaxBytes: 262144,
});
const MAX_OUTPUT_BYTES = 16 * 1024 * 1024;
const STARTUP_TIMEOUT_MS = 15000;

export const name = "beforedone";
export const inject = ["sessions", "subprocess"];
export const Config = z.object({
  binary: z.string().default(DEFAULTS.binary),
  gateTimeoutMs: z.number().step(1).min(1).default(DEFAULTS.gateTimeoutMs),
  stdoutMaxBytes: z.number().step(1).min(1).max(MAX_OUTPUT_BYTES).default(DEFAULTS.stdoutMaxBytes),
  stderrMaxBytes: z.number().step(1).min(1).max(MAX_OUTPUT_BYTES).default(DEFAULTS.stderrMaxBytes),
});

function normalizeConfig(config = {}) {
  const resolved = { ...DEFAULTS, ...config };
  if (typeof resolved.binary !== "string" || resolved.binary.trim().length === 0) {
    throw new Error("dsh-beforedone: `binary` must be a non-empty executable name or absolute path");
  }
  for (const key of ["gateTimeoutMs", "stdoutMaxBytes", "stderrMaxBytes"]) {
    const value = resolved[key];
    if (!Number.isSafeInteger(value) || value < 1) {
      throw new Error(`dsh-beforedone: \`${key}\` must be a positive safe integer`);
    }
  }
  for (const key of ["stdoutMaxBytes", "stderrMaxBytes"]) {
    if (resolved[key] > MAX_OUTPUT_BYTES) {
      throw new Error(`dsh-beforedone: \`${key}\` must not exceed ${MAX_OUTPUT_BYTES}`);
    }
  }
  return Object.freeze(resolved);
}

function workingDirectory(session) {
  return session.header?.cwd || process.cwd();
}

function formatError(error) {
  return error instanceof Error ? error.message : String(error);
}

function gateMessage(text) {
  return createUserMessage({
    content: [{ type: "text", text }],
    source: {
      kind: "plugin",
      plugin: "beforedone",
      form: "notice",
      summary: "BeforeDone completion gate",
    },
  });
}

export async function apply(ctx, suppliedConfig) {
  const config = normalizeConfig(suppliedConfig);
  const startupConfig = {
    ...config,
    gateTimeoutMs: Math.min(config.gateTimeoutMs, STARTUP_TIMEOUT_MS),
  };
  const binary = await resolveCliBinary(ctx, config.binary, startupConfig.gateTimeoutMs);
  const runtime = { ctx, binary, config: startupConfig };
  const version = await inspectCli(runtime, process.cwd());
  runtime.config = config;

  const states = new Map();
  const settling = new Set();
  ctx.logger.info(`dsh-beforedone activated with BeforeDone ${version.raw}`);

  function stateFor(session) {
    let state = states.get(session);
    if (state) return state;
    state = {
      session,
      lifecycleId: randomUUID(),
      pending: [],
      seen: new Set(),
      toolNames: new Map(),
      currentTurn: undefined,
      stopAttempts: new Map(),
      steeredTurns: new Set(),
      flushChain: Promise.resolve(),
      lastGap: undefined,
    };
    states.set(session, state);
    return state;
  }

  function enqueue(state, event) {
    if (state.seen.has(event.id)) return false;
    state.seen.add(event.id);
    state.pending.push(event);
    return true;
  }

  function flushState(state, signal) {
    state.flushChain = state.flushChain.catch(() => undefined).then(async () => {
      if (state.pending.length === 0) return;
      const batch = state.pending.slice();
      try {
        await ingestEvents(runtime, workingDirectory(state.session), batch, signal);
      } catch (error) {
        state.lastGap = formatError(error);
        ctx.logger.error(
          `dsh-beforedone capture gap for session ${String(state.session.id)}: ${state.lastGap}`,
        );
        throw new Error(`dsh-beforedone could not persist ${batch.length} event(s)`, {
          cause: error,
        });
      }
      state.pending.splice(0, batch.length);
      state.lastGap = undefined;
    });
    return state.flushChain;
  }

  function scheduleFinalFlush(state) {
    const pending = flushState(state).catch((error) => {
      ctx.logger.error(
        `dsh-beforedone final capture gap for session ${String(state.session.id)}: ${formatError(error)}`,
      );
    });
    settling.add(pending);
    void pending.finally(() => {
      settling.delete(pending);
      states.delete(state.session);
    });
  }

  ctx.on("agent/session-start", ({ agent, source }) => {
    const state = stateFor(agent.session);
    enqueue(state, createSessionStartedEvent(agent.session, source, state.lifecycleId));
  });

  ctx.on("session/event", (session, event) => {
    const state = stateFor(session);
    if (event.type === "turn/start") {
      state.currentTurn = event.data.turn;
      return;
    }
    if (event.type === "turn/end") {
      state.stopAttempts.delete(event.data.turn);
      state.steeredTurns.delete(event.data.turn);
      if (state.currentTurn === event.data.turn) state.currentTurn = undefined;
      return;
    }

    let toolName;
    if (event.type === "tool/call") {
      state.toolNames.set(String(event.data.callId), event.data.name);
    } else if (event.type === "tool/result") {
      const block = event.data.message?.content?.[0];
      const callId = String(event.data.message?.source?.callId ?? block?.toolCallId ?? "unknown");
      toolName = state.toolNames.get(callId);
      state.toolNames.delete(callId);
    }
    const projected = projectSessionEvent(session, event, state.currentTurn, toolName);
    if (projected) enqueue(state, projected);
  });

  ctx.on("session/flush", async (session) => {
    await flushState(stateFor(session));
  });

  ctx.on("agent/turn-stopping", async ({ agent, turn, signal }) => {
    signal.throwIfAborted();
    const state = stateFor(agent.session);
    const attempt = (state.stopAttempts.get(turn) || 0) + 1;
    state.stopAttempts.set(turn, attempt);
    enqueue(state, createAgentStoppingEvent(agent.session, turn, attempt, state.lifecycleId));

    let gate;
    let failure;
    try {
      await flushState(state, signal);
      gate = await evaluateGate(runtime, workingDirectory(agent.session), signal);
    } catch (error) {
      signal.throwIfAborted();
      failure = error;
    }

    if (gate?.decision === "allow") {
      if (gate.verdict === "INCONCLUSIVE") {
        ctx.logger.warn(
          `dsh-beforedone allowed session ${String(agent.session.id)} with an INCONCLUSIVE warning: ${gate.system_message}`,
        );
      }
      return;
    }

    if (!state.steeredTurns.has(turn)) {
      state.steeredTurns.add(turn);
      agent.steer(gateMessage(gate ? gateSteeringText(gate) : failureSteeringText(failure)));
      ctx.logger.warn(
        `dsh-beforedone requested one corrective continuation for session ${String(agent.session.id)} turn ${turn}`,
      );
      return;
    }

    const detail = gate?.reason || formatError(failure);
    ctx.logger.error(
      `dsh-beforedone still could not accept completion after the bounded retry for session ${String(agent.session.id)} turn ${turn}: ${detail}`,
    );
  });

  ctx.on("session/disposed", (session) => {
    const state = stateFor(session);
    enqueue(state, createSessionEndedEvent(session, state.lifecycleId));
    scheduleFinalFlush(state);
  });

  ctx.effect(
    () => async () => {
      const flushes = [...states.values()].map((state) =>
        flushState(state).catch((error) => {
          ctx.logger.error(
            `dsh-beforedone unload capture gap for session ${String(state.session.id)}: ${formatError(error)}`,
          );
        }),
      );
      await Promise.all([...flushes, ...settling]);
      states.clear();
    },
    "dsh-beforedone.flush-on-dispose",
  );
}
