import { createHash } from "node:crypto";

export const SOURCE = "deepseek-harness";
export const MINIMUM_CLI_VERSION = "1.1.1";
export const MAXIMUM_CLI_MAJOR = 2;
const MAX_EVENT_ID_CHARS = 256;
const MAX_STEERING_CHARS = 12000;

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function requireString(value, field) {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`BeforeDone returned an invalid ${field}`);
  }
  return value;
}

function boundedEventId(parts) {
  const raw = parts.map((part) => String(part)).join(":");
  if (raw.length <= MAX_EVENT_ID_CHARS) return raw;
  const digest = createHash("sha256").update(raw).digest("hex");
  return `${raw.slice(0, MAX_EVENT_ID_CHARS - digest.length - 1)}:${digest}`;
}

function occurredAt(milliseconds = Date.now()) {
  const date = new Date(milliseconds);
  if (!Number.isFinite(date.getTime())) {
    throw new Error("DeepSeek Harness emitted an invalid event time");
  }
  return date.toISOString();
}

function baseEvent(session, type, idParts, milliseconds = Date.now()) {
  const event = {
    schema_version: 1,
    id: boundedEventId(["dsh", session.id, ...idParts]),
    occurred_at: occurredAt(milliseconds),
    type,
    source: SOURCE,
    session_id: String(session.id),
  };
  if (session.header?.cwd) event.cwd = session.header.cwd;
  return event;
}

function turnId(session, turn) {
  return boundedEventId([session.id, "turn", turn]);
}

function eventAttributes(event, extra = {}) {
  return {
    dsh_event_type: event.type,
    dsh_event_seq: String(event.seq),
    ...extra,
  };
}

export function createSessionStartedEvent(session, source, lifecycleId) {
  return {
    ...baseEvent(session, "SessionStarted", ["session-start", lifecycleId, source]),
    summary: "DeepSeek Harness agent session started",
    attributes: {
      start_source: String(source),
      lifecycle_id: String(lifecycleId),
    },
  };
}

export function createAgentStoppingEvent(session, turn, attempt, lifecycleId) {
  return {
    ...baseEvent(session, "AgentStopping", ["lifecycle", lifecycleId, "turn", turn, "stopping", attempt]),
    turn_id: turnId(session, turn),
    summary: "DeepSeek Harness agent reached its completion boundary",
    attributes: {
      lifecycle_id: String(lifecycleId),
      stopping_attempt: String(attempt),
    },
  };
}

export function createSessionEndedEvent(session, lifecycleId) {
  return {
    ...baseEvent(session, "SessionEnded", ["session-ended", lifecycleId, session.seq]),
    summary: "DeepSeek Harness session ended",
    attributes: {
      final_seq: String(session.seq),
      lifecycle_id: String(lifecycleId),
    },
  };
}

export function projectSessionEvent(session, event, currentTurn, toolName) {
  switch (event.type) {
    case "user/message": {
      const source = event.data.message?.source ?? event.data.source ?? {};
      const sourceKind = source.kind ?? "unknown";
      const messageId = event.data.message?.id ?? event.data.id;
      const attributes = eventAttributes(event, { message_source: String(sourceKind) });
      if (source.plugin !== undefined) attributes.message_plugin = String(source.plugin);
      const projected = {
        ...baseEvent(session, "PromptSubmitted", ["event", event.seq, "prompt"], event.time),
        summary: "DeepSeek Harness accepted a model-visible user message",
        attributes,
      };
      if (messageId !== undefined) projected.attributes.message_id = String(messageId);
      if (currentTurn !== undefined) projected.turn_id = turnId(session, currentTurn);
      return projected;
    }
    case "tool/call":
      return {
        ...baseEvent(session, "ToolStarted", ["event", event.seq, "tool-started"], event.time),
        turn_id: turnId(session, event.data.turn),
        tool_name: event.data.name,
        summary: "DeepSeek Harness started a tool call",
        attributes: eventAttributes(event, {
          call_id: String(event.data.callId),
          step: String(event.data.step),
        }),
      };
    case "tool/result": {
      const block = event.data.message?.content?.[0];
      const callId = event.data.message?.source?.callId ?? block?.toolCallId ?? "unknown";
      const failed = Boolean(event.data.error || block?.isError);
      const projected = {
        ...baseEvent(session, "ToolFinished", ["event", event.seq, "tool-finished"], event.time),
        turn_id: turnId(session, event.data.turn),
        exit_code: failed ? 1 : 0,
        summary: "DeepSeek Harness finished a tool call",
        attributes: eventAttributes(event, {
          call_id: String(callId),
          step: String(event.data.step),
          outcome: failed ? "error" : "success",
        }),
      };
      if (toolName) projected.tool_name = toolName;
      return projected;
    }
    default:
      return undefined;
  }
}

export function parseCliVersion(output) {
  const match = /^beforedone\s+v?(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?\s*$/.exec(output);
  if (!match) {
    throw new Error("BeforeDone version output is not a supported semantic version");
  }
  return {
    raw: output.trim().replace(/^beforedone\s+/, ""),
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
  };
}

export function assertCompatibleCliVersion(output) {
  const version = parseCliVersion(output);
  const atLeastMinimum =
    version.major > 1 ||
    (version.major === 1 &&
      (version.minor > 1 || (version.minor === 1 && version.patch >= 1)));
  if (!atLeastMinimum || version.major >= MAXIMUM_CLI_MAJOR) {
    throw new Error(
      `dsh-beforedone requires BeforeDone CLI >=${MINIMUM_CLI_VERSION} <${MAXIMUM_CLI_MAJOR}; found ${version.raw}`,
    );
  }
  return version;
}

export function parseIngestResult(output, expectedEvents) {
  let value;
  try {
    value = JSON.parse(output);
  } catch (error) {
    throw new Error("BeforeDone adapter ingest returned malformed JSON", { cause: error });
  }
  if (!isRecord(value) || value.schema_version !== 1 || !Number.isInteger(value.ingested)) {
    throw new Error("BeforeDone adapter ingest returned an invalid result envelope");
  }
  if (!Array.isArray(value.event_ids) || value.event_ids.some((id) => typeof id !== "string")) {
    throw new Error("BeforeDone adapter ingest returned invalid event IDs");
  }
  const expectedIds = expectedEvents.map((event) => event.id);
  if (
    value.ingested !== expectedEvents.length ||
    value.event_ids.length !== expectedIds.length ||
    value.event_ids.some((id, index) => id !== expectedIds[index])
  ) {
    throw new Error("BeforeDone adapter ingest did not acknowledge the complete event batch");
  }
  return value;
}

export function parseGateResult(output) {
  let value;
  try {
    value = JSON.parse(output);
  } catch (error) {
    throw new Error("BeforeDone gate returned malformed JSON", { cause: error });
  }
  if (!isRecord(value) || value.schema_version !== 1) {
    throw new Error("BeforeDone gate returned an invalid result envelope");
  }
  if (value.decision !== "allow" && value.decision !== "block") {
    throw new Error("BeforeDone gate returned an invalid decision");
  }
  if (!new Set(["PASS", "FAIL", "INCONCLUSIVE"]).has(value.verdict)) {
    throw new Error("BeforeDone gate returned an invalid verdict");
  }
  if (!Array.isArray(value.checks)) {
    throw new Error("BeforeDone gate returned invalid check results");
  }
  for (const check of value.checks) {
    if (
      !isRecord(check) ||
      typeof check.check_id !== "string" ||
      !new Set(["PASS", "FAIL", "INCONCLUSIVE"]).has(check.verdict)
    ) {
      throw new Error("BeforeDone gate returned an invalid check result");
    }
  }
  if (value.decision === "block") requireString(value.reason, "block reason");
  if (value.verdict === "PASS" && value.decision !== "allow") {
    throw new Error("BeforeDone gate returned an inconsistent PASS decision");
  }
  if (value.verdict === "FAIL" && value.decision !== "block") {
    throw new Error("BeforeDone gate returned an inconsistent FAIL decision");
  }
  if (value.decision === "allow" && value.verdict === "INCONCLUSIVE") {
    requireString(value.system_message, "INCONCLUSIVE system message");
  }
  return value;
}

function boundSteeringText(value) {
  if (value.length <= MAX_STEERING_CHARS) return value;
  return `${value.slice(0, MAX_STEERING_CHARS)}\n[BeforeDone message truncated]`;
}

export function gateSteeringText(result) {
  const reason = result.reason || "Required evidence is not fresh and conclusive.";
  return boundSteeringText(
    `BeforeDone cannot safely accept completion yet.\n\n${reason}\n\nRun the required checks with \`beforedone check <check-id>\`, address any failure, and only finish after a fresh \`beforedone gate --json\` result allows completion.`,
  );
}

export function failureSteeringText(error) {
  const detail = error instanceof Error ? error.message : String(error);
  return boundSteeringText(
    `BeforeDone could not safely verify completion because its local gate failed: ${detail}\n\nDiagnose the local BeforeDone CLI or evidence configuration, then rerun the required checks before finishing.`,
  );
}
