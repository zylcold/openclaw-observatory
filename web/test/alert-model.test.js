import test from "node:test";
import assert from "node:assert/strict";
import { applyAlertState, evaluateAlerts } from "../src/alert-model.js";

test("evaluates deduplicated operational, stuck-session, system and budget alerts", () => {
  const now = Date.parse("2026-07-18T12:00:00Z");
  const alerts = evaluateAlerts({
    agents: [{ agentId: "main", runs: 10, runErrors: 2 }],
    models: [{ provider: "openai", model: "gpt", requests: 10, errors: 0, errorRate: 0, averageDurationMs: 6000 }],
    tools: [{ source: "tool", tool: "exec", calls: 10, errors: 2, failureRate: 20, p99DurationMs: 20000 }],
    sessions: [{ sessionId: "s1", agentId: "main", status: "active", startedAt: "2026-07-18T10:00:00Z" }],
    anomalies: [{ kind: "system", id: "crash-1", label: "gateway.crashed", category: "crash", occurredAt: "2026-07-18T11:00:00Z" }],
    costSummary: { lastMonthCost: 90 },
  }, { thresholds: { errorRateWarning: 5, errorRateCritical: 15, llmLatencyWarningMs: 5000, llmLatencyCriticalMs: 15000, toolP99WarningMs: 5000, toolP99CriticalMs: 15000, sessionStuckMs: 3600000, costBudgetUsd: 100 } }, now);
  assert.ok(alerts.some((item) => item.id === "agent-error:main" && item.severity === "critical"));
  assert.ok(alerts.some((item) => item.id === "session-stuck:s1"));
  assert.ok(alerts.some((item) => item.id === "system:crash-1"));
  assert.ok(alerts.some((item) => item.id === "cost-budget:monthly"));
  assert.equal(new Set(alerts.map((item) => item.id)).size, alerts.length);
});

test("applies acknowledge and silence state without mutating alert identity", () => {
  const alerts = [{ id: "a", severity: "warning" }, { id: "b", severity: "critical" }];
  const decorated = applyAlertState(alerts, { acknowledged: { a: "now" }, silenced: { b: 2000 } }, 1000);
  assert.equal(decorated[0].acknowledged, true);
  assert.equal(decorated[1].silenced, true);
  assert.deepEqual(alerts[0], { id: "a", severity: "warning" });
});
