import test from "node:test";
import assert from "node:assert/strict";
import { Forwarder, privacy } from "../lib/forwarder.js";
import { buildQueryURL } from "../lib/query.js";

test("maps diagnostics without prompt or tool content", () => {
  const f = new Forwarder({ queueCapacity: 100 });
  f.mapDiagnostic({ type: "tool.execution.started", ts: Date.now(), seq: 9, runId: "r1", sessionId: "s1", toolCallId: "t1", toolName: "exec", toolSource: "core", params: { command: "secret" }, toolOutput: "secret" });
  assert.equal(f.queue.length, 1);
  const event = f.queue[0].event;
  assert.equal(event.eventType, "tool.started");
  assert.equal(event.payload.toolName, "exec");
  assert.equal("params" in event.payload, false);
  assert.equal(JSON.stringify(event).includes("secret"), false);
});

test("maps stable trace spans, retry metadata and LLM timing without content", () => {
  const f = new Forwarder({ queueCapacity: 100 });
  f.mapDiagnostic({
    type: "model.call.completed", ts: Date.now(), runId: "run-1", sessionId: "session-1", callId: "call-1",
    provider: "openai", model: "gpt", attempt: 2, retryReason: "rate_limit",
    timeToFirstTokenMs: 120, generationDurationMs: 800, stopReason: "stop",
    prompt: "private prompt", response: "private response",
  });
  const payload = f.queue[0].event.payload;
  assert.equal(payload.traceId, "run-1");
  assert.equal(payload.spanId, "call-1");
  assert.equal(payload.parentSpanId, "run-1");
  assert.equal(payload.attempt, 2);
  assert.equal(payload.timeToFirstTokenMs, 120);
  assert.equal(payload.generationDurationMs, 800);
  assert.equal(JSON.stringify(payload).includes("private"), false);

  f.mapDiagnostic({
    type: "model.failover", ts: Date.now(), runId: "run-1", sessionId: "session-1",
    callId: "retry-1", fromModel: "a", toModel: "b", attempt: 2, reason: "rate_limit",
  });
  assert.equal(f.queue[1].event.eventType, "llm.retried");
  assert.equal(f.queue[1].event.payload.spanId, "retry-1");
  assert.equal(f.queue[1].event.payload.reason, "rate_limit");
});

test("drops low priority first when full", () => {
  const f = new Forwarder({ queueCapacity: 100 });
  for (let i = 0; i < 100; i++) f.enqueue("gateway.heartbeat", { i }, "low");
  assert.equal(f.enqueue("gateway.stopped", {}, "critical"), true);
  assert.equal(f.queue.length, 100);
  assert.equal(f.queue.at(-1).event.eventType, "gateway.stopped");
});

test("retains critical events when the queue only contains critical events", () => {
  const f = new Forwarder({ queueCapacity: 3 });
  for (let i = 0; i < 3; i++) f.enqueue("session.completed", { i }, "critical");
  assert.equal(f.enqueue("gateway.stopped", {}, "critical"), true);
  assert.equal(f.queue.length, 4);
  assert.equal(f.queue.at(-1).event.eventType, "gateway.stopped");
});

test("probes the socket before posting and backs off with jitter", async () => {
  const f = new Forwarder({ queueCapacity: 100 });
  f.enqueue("gateway.heartbeat", {}, "low");
  f.socketAvailable = async () => false;
  f.post = () => assert.fail("post should not run when socket probe fails");
  const before = Date.now();
  assert.equal(await f.flush(), false);
  assert.equal(f.failureCount, 1);
  assert.ok(f.nextAttemptAt >= before + 125 && f.nextAttemptAt <= before + 250);
});

test("caps retry backoff at 30 seconds", () => {
  const f = new Forwarder();
  for (let i = 0; i < 20; i++) f.scheduleRetry();
  const delay = f.nextAttemptAt - Date.now();
  assert.ok(delay >= 15_000 && delay <= 30_000);
});

test("hashes session keys", () => {
  const f = new Forwarder();
  const hashed = f.sessionKeyHash("telegram:user:123");
  assert.match(hashed, /^sha256:[a-f0-9]{24}$/);
  assert.equal(hashed.includes("telegram"), false);
});

test("extracts the OpenClaw agent id without forwarding the raw session key", () => {
  assert.equal(privacy.agentIdFromSessionKey("agent:tom:telegram:dm:123"), "tom");
  assert.equal(privacy.agentIdFromSessionKey("legacy-session-key"), undefined);

  const f = new Forwarder();
  f.mapDiagnostic({
    type: "run.started", ts: Date.now(), runId: "r-tom",
    sessionKey: "agent:tom:telegram:dm:123", sessionId: "s-tom",
  });
  assert.equal(f.queue[0].event.payload.agentId, "tom");
  assert.equal(JSON.stringify(f.queue[0].event).includes("telegram:dm:123"), false);
});

test("builds only fixed localhost Observatory URLs", () => {
  const url = buildQueryURL({ resource: "events", limit: 500, eventType: "tool.failed" });
  assert.equal(url.origin, "http://127.0.0.1:10086");
  assert.equal(url.pathname, "/api/v1/events");
  assert.equal(url.searchParams.get("limit"), "100");
  assert.equal(url.searchParams.get("eventType"), "tool.failed");
  assert.throws(() => buildQueryURL({ resource: "run" }), /id is required/);
});

test("matches model usage by stable session key when session ids differ", () => {
  const f = new Forwarder();
  f.mapDiagnostic({
    type: "model.call.completed", ts: Date.now(), runId: "r1", sessionId: "embedded-session",
    sessionKey: "agent:main:cron:1", callId: "c1", provider: "bailian", model: "qwen3.7-plus",
  });
  f.mapDiagnostic({
    type: "model.usage", ts: Date.now(), sessionId: "run-session", sessionKey: "agent:main:cron:1",
    provider: "bailian", model: "qwen3.7-plus", lastCallUsage: { input: 90, output: 10, cacheRead: 200 },
  });
  assert.equal(f.queue.length, 2);
  assert.equal(f.queue[1].event.payload.callId, "c1");
  assert.equal(f.queue[1].event.payload.inputTokens, 90);
  assert.equal(f.queue[1].event.payload.cacheReadTokens, 200);
});

test("maps llm output usage to the last call by run id without content", () => {
  const f = new Forwarder();
  f.mapDiagnostic({
    type: "model.call.completed", ts: Date.now(), runId: "r-output", sessionId: "s1",
    callId: "c-output", provider: "bailian", model: "qwen3.7-plus",
  });
  const mapped = f.mapUsage({
    runId: "r-output", sessionId: "s1", provider: "bailian", model: "qwen3.7-plus",
    usage: { input: 50431, output: 21, cacheRead: 0, cacheWrite: 0 },
    assistantTexts: ["secret response"], prompt: "secret prompt",
  });
  assert.equal(mapped, true);
  assert.equal(f.queue[1].event.payload.callId, "c-output");
  assert.equal(f.queue[1].event.payload.inputTokens, 50431);
  assert.equal(f.queue[1].event.payload.outputTokens, 21);
  assert.equal(JSON.stringify(f.queue[1].event).includes("secret"), false);
});

test("emits an uncorrelated accounting update when hook and service state differ", () => {
  const f = new Forwarder();
  const mappedImmediately = f.mapUsage({
    runId: "r-race", sessionId: "s-race", provider: "bailian", model: "qwen3.7-plus",
    usage: { input: 46226, output: 397, cacheRead: 4224, cacheWrite: 0 },
  });
  assert.equal(mappedImmediately, true);
  assert.equal(f.queue.length, 1);
  assert.equal(f.queue[0].event.eventType, "llm.completed");
  assert.equal(f.queue[0].event.payload.callId, undefined);
  assert.equal(f.queue[0].event.payload.runId, "r-race");
  assert.equal(f.queue[0].event.payload.accountingUpdate, true);
  assert.equal(f.queue[0].event.payload.inputTokens, 46226);
  assert.equal(f.queue[0].event.payload.cacheReadTokens, 4224);
});
