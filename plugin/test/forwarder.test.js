import test from "node:test";
import assert from "node:assert/strict";
import { Forwarder } from "../lib/forwarder.js";
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

test("drops low priority first when full", () => {
  const f = new Forwarder({ queueCapacity: 100 });
  for (let i = 0; i < 100; i++) f.enqueue("gateway.heartbeat", { i }, "low");
  assert.equal(f.enqueue("gateway.stopped", {}, "critical"), true);
  assert.equal(f.queue.length, 100);
  assert.equal(f.queue.at(-1).event.eventType, "gateway.stopped");
});

test("hashes session keys", () => {
  const f = new Forwarder();
  const hashed = f.sessionKeyHash("telegram:user:123");
  assert.match(hashed, /^sha256:[a-f0-9]{24}$/);
  assert.equal(hashed.includes("telegram"), false);
});

test("builds only fixed localhost Observatory URLs", () => {
  const url = buildQueryURL({ resource: "events", limit: 500, eventType: "tool.failed" });
  assert.equal(url.origin, "http://127.0.0.1:10086");
  assert.equal(url.pathname, "/api/v1/events");
  assert.equal(url.searchParams.get("limit"), "100");
  assert.equal(url.searchParams.get("eventType"), "tool.failed");
  assert.throws(() => buildQueryURL({ resource: "run" }), /id is required/);
});
