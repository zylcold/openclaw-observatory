import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { onInternalDiagnosticEvent } from "openclaw/plugin-sdk/diagnostic-runtime";
import { Forwarder } from "./lib/forwarder.js";
import { queryObservatory } from "./lib/query.js";

export default definePluginEntry({
  id: "openclaw-observatory",
  name: "OpenClaw Observatory",
  description: "Forwards bounded privacy-safe runtime metadata to the local Observatory daemon.",
  register(api) {
    const forwarder = new Forwarder(api.pluginConfig || {}, api.logger);
    let unsubscribeDiagnostics;

    api.registerTool({
      name: "observatory_query",
      description: "Read privacy-safe metadata from the local OpenClaw Observatory service. Use for Gateway health, sessions, runs, LLM/tool statistics, resource usage, and recent events. This tool is read-only.",
      parameters: {
        type: "object",
        additionalProperties: false,
        required: ["resource"],
        properties: {
          resource: { type: "string", enum: ["status", "instances", "sessions", "session", "runs", "run", "resources", "tools", "models", "events"] },
          id: { type: "string", maxLength: 256, description: "Required for session or run detail." },
          limit: { type: "integer", minimum: 1, maximum: 100 },
          instanceId: { type: "string", maxLength: 128 },
          status: { type: "string", enum: ["active", "completed", "failed", "unknown"] },
          eventType: { type: "string", maxLength: 64 },
          from: { type: "string", maxLength: 35, description: "UTC RFC3339 lower time bound." },
          to: { type: "string", maxLength: 35, description: "UTC RFC3339 upper time bound." }
        }
      },
      async execute(_toolCallId, params) {
        try {
          const result = await queryObservatory(params);
          return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
        } catch (error) {
          const message = error instanceof Error ? error.message : "Observatory query failed";
          return { isError: true, content: [{ type: "text", text: message }] };
        }
      }
    });

    api.on("session_start", (evt, ctx) => {
      forwarder.enqueue("session.started", {
        sessionId: evt.sessionId,
        sessionKeyHash: forwarder.sessionKeyHash(evt.sessionKey),
        agentId: ctx?.agentId,
        resumedFromHash: forwarder.sessionKeyHash(evt.resumedFrom),
      });
    });
    api.on("session_end", (evt, ctx) => {
      const failed = evt.reason === "unknown";
      forwarder.enqueue(failed ? "session.failed" : "session.completed", {
        sessionId: evt.sessionId, sessionKeyHash: forwarder.sessionKeyHash(evt.sessionKey),
        agentId: ctx?.agentId,
        reason: evt.reason, messageCount: evt.messageCount, durationMs: evt.durationMs,
      }, "critical");
    });
    api.on("subagent_spawned", (evt, ctx) => {
      const childSessionHash = forwarder.sessionKeyHash(evt.childSessionKey);
      const subagentId = evt.runId || childSessionHash;
      if (childSessionHash) forwarder.subagentBySession.set(childSessionHash, subagentId);
      forwarder.enqueue("subagent.started", {
        subagentId, runId: evt.runId, parentRunId: ctx?.runId,
        childSessionHash, agentId: evt.agentId, mode: evt.mode,
        provider: evt.resolvedProvider, model: evt.resolvedModel,
      });
    });
    api.on("subagent_ended", (evt) => {
      const childSessionHash = forwarder.sessionKeyHash(evt.targetSessionKey);
      const subagentId = forwarder.subagentBySession.get(childSessionHash) || evt.runId || childSessionHash;
      const failed = evt.outcome && evt.outcome !== "ok";
      forwarder.enqueue(failed ? "subagent.failed" : "subagent.completed", {
        subagentId, runId: evt.runId, childSessionHash, targetKind: evt.targetKind,
        reason: evt.reason, outcome: evt.outcome,
      }, "critical", evt.endedAt);
      if (childSessionHash) forwarder.subagentBySession.delete(childSessionHash);
    });
    // Requires hooks.allowConversationAccess=true. Conversation fields are
    // deliberately ignored; only identifiers and normalized usage are copied.
    api.on("llm_output", (evt, ctx) => {
      // OpenClaw may instantiate typed hooks separately from plugin services;
      // ensure this hook-owned forwarder has an active asynchronous flusher.
      forwarder.start();
      forwarder.mapUsage({
        runId: evt.runId,
        sessionId: evt.sessionId || ctx?.sessionId,
        sessionKey: ctx?.sessionKey,
        provider: evt.provider,
        model: evt.model,
        usage: evt.usage,
      });
    });

    api.registerService({
      id: "openclaw-observatory-forwarder",
      start(ctx) {
        forwarder.start();
        if (ctx.internalDiagnostics?.onEvent) {
          unsubscribeDiagnostics = ctx.internalDiagnostics.onEvent((evt) => forwarder.mapDiagnostic(evt));
        } else {
          unsubscribeDiagnostics = onInternalDiagnosticEvent((evt) => forwarder.mapDiagnostic(evt));
        }
        forwarder.enqueue("gateway.started", {
          pluginVersion: "0.1.0", openclawVersion: process.env.OPENCLAW_VERSION || "2026.6.11+",
          protocolVersions: [1], capabilities: ["diagnostics", "session_hooks", "subagent_hooks", "mcp_source"],
        }, "critical");
        void forwarder.flush();
      },
      async stop() {
        unsubscribeDiagnostics?.();
        forwarder.enqueue("gateway.stopped", { reason: "plugin_service_stop" }, "critical");
        await Promise.race([forwarder.flush(true), new Promise((resolve) => setTimeout(resolve, 275))]);
        forwarder.stopTimer();
      },
    });
  },
});
