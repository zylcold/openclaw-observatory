import test from "node:test";
import assert from "node:assert/strict";

import {
  agentStatus, chartsForDomain, filterDashboardData, normalizeDomain, percentile,
  sessionSummary,
} from "../src/observability-model.js";

test("observability domains and chart routing are normalized", () => {
  assert.equal(normalizeDomain("models"), "models");
  assert.equal(normalizeDomain("missing"), "overview");
  assert.deepEqual(chartsForDomain([
    { id: "a", domain: "agents" },
    { id: "m", domain: "models" },
  ], "models").map((chart) => chart.id), ["m"]);
});

test("context filters preserve dashboard shape and filter matching detail rows", () => {
  const data = {
    sessions: [{ status: "active" }, { status: "failed" }],
    llmCalls: [{ model: "gpt-5", status: "failed" }, { model: "claude", status: "completed" }],
    subagents: [],
    mcpCalls: [{ toolName: "search", status: "failed" }],
    models: [{ model: "gpt-5" }, { model: "claude" }],
    tools: [{ tool: "search" }, { tool: "shell" }],
    timeseries: { models: [{ model: "gpt-5" }], tools: [{ tool: "search" }], points: [1] },
  };
  const result = filterDashboardData(data, { model: "gpt-5", tool: "search", status: "failed" });
  assert.equal(result.sessions.length, 1);
  assert.equal(result.llmCalls.length, 1);
  assert.equal(result.models.length, 1);
  assert.equal(result.tools.length, 1);
  assert.deepEqual(result.timeseries.points, [1]);
});

test("session percentiles and agent state use active sessions", () => {
  const now = Date.parse("2026-07-18T01:00:00Z");
  const sessions = [
    { agentId: "main", status: "completed", startedAt: "2026-07-18T00:00:00Z", endedAt: "2026-07-18T00:00:01Z", messageCount: 2 },
    { agentId: "main", status: "active", startedAt: "2026-07-18T00:59:58Z", messageCount: 3 },
    { agentId: "other", status: "completed", startedAt: "2026-07-18T00:00:00Z", endedAt: "2026-07-18T00:00:10Z", messageCount: 1 },
  ];
  assert.equal(percentile([1000, 2000, 10000], 0.9), 10000);
  assert.deepEqual(sessionSummary(sessions, now), {
    total: 3, active: 1, messages: 6, p50: 2000, p90: 10000, p99: 10000,
  });
  assert.deepEqual(agentStatus({ agentId: "main", runErrors: 0 }, sessions), {
    state: "active", activeSessions: 1, sessions: 2,
  });
});
