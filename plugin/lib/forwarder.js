import { createHash, randomUUID } from "node:crypto";
import { access } from "node:fs/promises";
import { homedir, hostname } from "node:os";
import { request } from "node:http";
import { join } from "node:path";

const MAX_EVENT_BYTES = 16 * 1024;
const MAX_BATCH_BYTES = 1024 * 1024;
const PRIORITY = { low: 0, normal: 1, critical: 2 };

function hash(value) {
  return createHash("sha256").update(String(value)).digest("hex").slice(0, 24);
}

function cleanString(value, max = 256) {
  if (typeof value !== "string") return undefined;
  const cleaned = value.replace(/[\r\n\0]/g, " ");
  return cleaned.length > max ? cleaned.slice(0, max) : cleaned;
}

function cleanPayload(payload) {
  return Object.fromEntries(Object.entries(payload).filter(([, value]) => value !== undefined && value !== null && value !== ""));
}

function agentIdFromSessionKey(value) {
  const sessionKey = cleanString(value);
  if (!sessionKey) return undefined;
  const match = /^agent:([^:]+):/i.exec(sessionKey);
  return cleanString(match?.[1], 128);
}

export class Forwarder {
  constructor(config = {}, logger = console) {
    this.socketPath = config.socketPath || join(homedir(), ".openclaw-observatory", "observatory.sock");
    this.capacity = config.queueCapacity || 10_000;
    this.flushIntervalMs = config.flushIntervalMs || 250;
    this.requestTimeoutMs = config.requestTimeoutMs || 5_000;
    this.forceFlushTimeoutMs = config.forceFlushTimeoutMs || 250;
    this.logger = logger;
    this.instanceId = `local-${hash(`${hostname()}|${homedir()}`)}`;
    this.producerId = `plugin-${randomUUID()}`;
    this.sequence = 0;
    this.queue = [];
    this.timer = undefined;
    this.inflight = false;
    this.failureCount = 0;
    this.nextAttemptAt = 0;
    this.dropped = 0;
    this.dropReason = "queue_full";
    this.lastCallBySession = new Map();
    this.subagentBySession = new Map();
  }

  start() {
    if (!this.timer) this.timer = setInterval(() => void this.flush(), this.flushIntervalMs).unref();
  }

  stopTimer() {
    if (this.timer) clearInterval(this.timer);
    this.timer = undefined;
  }

  sessionKeyHash(value) {
    return typeof value === "string" && value ? `sha256:${hash(value)}` : undefined;
  }

  enqueue(eventType, payload = {}, priority = "normal", occurredAt = Date.now()) {
    const event = {
      schemaVersion: 1,
      eventId: randomUUID(),
      eventType,
      occurredAt: new Date(Number.isFinite(occurredAt) ? occurredAt : Date.now()).toISOString(),
      instanceId: this.instanceId,
      producerId: this.producerId,
      processId: process.pid,
      sequence: ++this.sequence,
      source: "plugin",
      payload: cleanPayload(payload),
    };
    const body = JSON.stringify(event);
    if (Buffer.byteLength(body) > MAX_EVENT_BYTES) {
      this.recordDrop("event_too_large");
      return false;
    }
    if (this.queue.length >= this.capacity) {
      const incomingPriority = PRIORITY[priority] ?? PRIORITY.normal;
      let candidate = -1;
      for (let p = PRIORITY.low; p < incomingPriority && candidate < 0; p++) {
        candidate = this.queue.findIndex((entry) => entry.priority === p);
      }
      if (candidate < 0) {
        // A terminal lifecycle event is more valuable than the configured
        // in-memory limit. Keep it for the next daemon recovery rather than
        // losing the only record of a completed/crashed run.
        if (incomingPriority === PRIORITY.critical) {
          this.queue.push({ event, body, bytes: Buffer.byteLength(body), priority: incomingPriority });
          return true;
        }
        this.recordDrop("queue_full");
        return false;
      }
      this.queue.splice(candidate, 1);
      this.recordDrop("queue_full");
    }
    this.queue.push({ event, body, bytes: Buffer.byteLength(body), priority: PRIORITY[priority] ?? PRIORITY.normal });
    this.maybeQueueDropNotice();
    return true;
  }

  recordDrop(reason) {
    this.dropped += 1;
    this.dropReason = reason;
  }

  maybeQueueDropNotice() {
    if (!this.dropped || this.queue.length >= this.capacity) return;
    const count = this.dropped;
    const reason = this.dropReason;
    this.dropped = 0;
    this.enqueue("monitor.events_dropped", { count, reason, queueDepth: this.queue.length }, "critical");
  }

  mapDiagnostic(evt) {
    if (!evt || typeof evt.type !== "string") return;
    const base = {
      runId: cleanString(evt.runId), sessionId: cleanString(evt.sessionId),
      sessionKeyHash: this.sessionKeyHash(evt.sessionKey), provider: cleanString(evt.provider, 128),
      model: cleanString(evt.model, 256), channel: cleanString(evt.channel, 64),
      agentId: cleanString(evt.agentId, 128) || agentIdFromSessionKey(evt.sessionKey),
      traceId: cleanString(evt.traceId) || cleanString(evt.runId) || cleanString(evt.sessionId),
    };
    const runTrace = {
      ...base,
      spanId: cleanString(evt.spanId) || cleanString(evt.runId),
      parentSpanId: cleanString(evt.parentSpanId),
    };
    const callTrace = (id) => ({
      ...base,
      spanId: cleanString(evt.spanId) || cleanString(id),
      parentSpanId: cleanString(evt.parentSpanId) || cleanString(evt.runId),
      attempt: Number.isFinite(evt.attempt) ? evt.attempt : undefined,
      retryReason: cleanString(evt.retryReason, 128),
    });
    switch (evt.type) {
      case "run.started":
        this.enqueue("agent.started", { ...runTrace, trigger: cleanString(evt.trigger, 64) }, "normal", evt.ts); break;
      case "run.completed":
        this.enqueue(evt.outcome === "completed" ? "agent.completed" : "agent.failed", {
          ...runTrace, trigger: cleanString(evt.trigger, 64), durationMs: evt.durationMs,
          outcome: cleanString(evt.outcome, 32), errorCategory: cleanString(evt.errorCategory, 64),
        }, "critical", evt.ts); break;
      case "model.call.started": {
        const value = { ...callTrace(evt.callId), callId: cleanString(evt.callId), api: cleanString(evt.api, 64), transport: cleanString(evt.transport, 64) };
        this.rememberCall(evt, value, "active"); this.enqueue("llm.started", value, "normal", evt.ts); break;
      }
      case "model.call.completed":
      case "model.call.error": {
        const failed = evt.type.endsWith("error");
        const value = { ...callTrace(evt.callId), callId: cleanString(evt.callId), api: cleanString(evt.api, 64), transport: cleanString(evt.transport, 64),
          durationMs: evt.durationMs, errorCategory: cleanString(evt.errorCategory, 64), failureKind: cleanString(evt.failureKind, 64),
          requestPayloadBytes: evt.requestPayloadBytes, responseStreamBytes: evt.responseStreamBytes,
          timeToFirstByteMs: evt.timeToFirstByteMs, timeToFirstTokenMs: evt.timeToFirstTokenMs,
          generationDurationMs: evt.generationDurationMs, stopReason: cleanString(evt.stopReason, 64) };
        this.rememberCall(evt, value, failed ? "failed" : "completed"); this.enqueue(failed ? "llm.failed" : "llm.completed", value, "critical", evt.ts); break;
      }
      case "model.usage": {
        this.mapUsage(evt); break;
      }
      case "model.failover":
        this.enqueue("llm.retried", { ...callTrace(evt.callId), sessionKeyHash: base.sessionKeyHash,
          fromProvider: cleanString(evt.fromProvider,128), fromModel: cleanString(evt.fromModel,256),
          toProvider: cleanString(evt.toProvider,128), toModel: cleanString(evt.toModel,256),
          reason: cleanString(evt.reason,128), attempt: Number.isFinite(evt.attempt) ? evt.attempt : 1,
        }, "normal", evt.ts); break;
      case "tool.execution.started":
      case "tool.execution.completed":
      case "tool.execution.error":
      case "tool.execution.blocked": {
        const suffix = evt.type.split(".").at(-1); const terminal = suffix !== "started";
        const family = evt.toolSource === "mcp" ? "mcp" : "tool";
        const state = suffix === "started" ? "started" : suffix === "completed" ? "completed" : "failed";
        const toolCallId = cleanString(evt.toolCallId) || `event-${evt.seq}`;
        this.enqueue(`${family}.${state}`, { ...callTrace(toolCallId), toolCallId,
          toolName: cleanString(evt.toolName,128) || "unknown", toolSource: cleanString(evt.toolSource,32),
          toolOwner: cleanString(evt.toolOwner,128), durationMs: evt.durationMs,
          errorCategory: cleanString(evt.errorCategory,64) || (suffix === "blocked" ? "blocked" : undefined),
        }, terminal ? "critical" : "normal", evt.ts); break;
      }
      case "diagnostic.heartbeat":
        this.enqueue("gateway.heartbeat", { active: evt.active, waiting: evt.waiting, queued: evt.queued, queueDepth: this.queue.length, queueCapacity: this.capacity }, "low", evt.ts); break;
      case "diagnostic.async_queue.dropped":
        this.enqueue("monitor.events_dropped", { count: evt.droppedEvents, reason: "openclaw_diagnostic_queue", queueDepth: evt.queueLength }, "critical", evt.ts); break;
    }
  }

  callKeys(evt) {
    return [...new Set([
      cleanString(evt.runId) ? `run:${cleanString(evt.runId)}` : undefined,
      this.sessionKeyHash(evt.sessionKey) ? `key:${this.sessionKeyHash(evt.sessionKey)}` : undefined,
      cleanString(evt.sessionId) ? `session:${cleanString(evt.sessionId)}` : undefined,
    ].filter(Boolean))];
  }

  rememberCall(evt, value, status) {
    const keys = this.callKeys(evt);
    for (const key of keys) {
      this.lastCallBySession.delete(key);
      this.lastCallBySession.set(key, { value, status });
    }
    while (this.lastCallBySession.size > 2000) this.lastCallBySession.delete(this.lastCallBySession.keys().next().value);

  }

  mapUsage(evt) {
    const keys = this.callKeys(evt);
    const last = keys.map((key) => this.lastCallBySession.get(key)).find(Boolean);
    const usage = evt.lastCallUsage || evt.lastUsage || evt.usage || {};
    if (!last) {
      this.enqueue("llm.completed", {
        runId: cleanString(evt.runId),
        sessionId: cleanString(evt.sessionId),
        sessionKeyHash: this.sessionKeyHash(evt.sessionKey),
        provider: cleanString(evt.provider, 128),
        model: cleanString(evt.model, 256),
        accountingUpdate: true,
        inputTokens: usage.input,
        outputTokens: usage.output,
        cacheReadTokens: usage.cacheRead,
        cacheWriteTokens: usage.cacheWrite,
        costUsd: evt.costUsd,
      }, "normal", evt.ts);
      return true;
    }
    this.enqueue(last.status === "failed" ? "llm.failed" : "llm.completed", {
      ...last.value,
      provider: cleanString(evt.provider, 128) || last.value.provider,
      model: cleanString(evt.model, 256) || last.value.model,
      accountingUpdate: true,
      inputTokens: usage.input,
      outputTokens: usage.output,
      cacheReadTokens: usage.cacheRead,
      cacheWriteTokens: usage.cacheWrite,
      costUsd: evt.costUsd,
    }, "normal", evt.ts);
    return true;
  }

  async flush(force = false) {
    if (this.inflight || !this.queue.length || (!force && Date.now() < this.nextAttemptAt)) return false;
    let bytes = 2; let count = 0;
    while (count < this.queue.length && count < 100) { const next = this.queue[count]; if (count && bytes + next.bytes + 1 > MAX_BATCH_BYTES) break; bytes += next.bytes + 1; count++; }
    const batch = this.queue.slice(0, count); const body = `[${batch.map((x) => x.body).join(",")}]`;
    this.inflight = true;
    try {
      if (!await this.socketAvailable()) {
        this.scheduleRetry();
        return false;
      }
      await this.post(body, force ? this.forceFlushTimeoutMs : this.requestTimeoutMs);
      this.queue.splice(0, count); this.failureCount = 0; this.nextAttemptAt = 0; this.maybeQueueDropNotice(); return true;
    } catch {
      this.scheduleRetry(); return false;
    } finally { this.inflight = false; }
  }

  async socketAvailable() {
    try { await access(this.socketPath); return true; } catch { return false; }
  }

  scheduleRetry() {
    this.failureCount++;
    const ceiling = Math.min(30_000, 250 * 2 ** Math.min(this.failureCount - 1, 7));
    const jittered = Math.floor(ceiling * (0.5 + Math.random() * 0.5));
    this.nextAttemptAt = Date.now() + jittered;
  }

  post(body, timeoutMs) {
    return new Promise((resolve, reject) => {
      const req = request({ socketPath: this.socketPath, path: "/v1/events", method: "POST", headers: { "content-type": "application/json", "content-length": Buffer.byteLength(body) } }, (res) => {
        res.resume(); res.on("end", () => res.statusCode >= 200 && res.statusCode < 300 ? resolve() : reject(new Error(`daemon status ${res.statusCode}`)));
      });
      req.setTimeout(timeoutMs, () => req.destroy(new Error("daemon timeout"))); req.on("error", reject); req.end(body);
    });
  }
}

export const privacy = { cleanString, cleanPayload, hash, agentIdFromSessionKey };
