const API = "/api/v1";

import { patchDashboardCosts, getPricing } from "./pricing.js";

function query(params = {}) {
  const q = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== "" && value !== null && value !== undefined) q.set(key, value);
  });
  return q.size ? `?${q}` : "";
}

async function get(path, params) {
  const response = await fetch(`${API}${path}${query(params)}`, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body?.error?.message || `${response.status} ${response.statusText}`);
  return body.data;
}

export async function loadDashboard(filters) {
  const common = { from: filters.from, to: filters.to, instanceId: filters.instanceId, agentId: filters.agentId };
  const list = { ...common, limit: 200 };
  // Always fetch 30d cost trends for Today/Week/Month KPIs (independent of dashboard filter)
  const now = new Date();
  const from30d = new Date(now.getTime() - 30 * 86400000).toISOString();
  const costCommon30d = { from: from30d, to: now.toISOString(), instanceId: filters.instanceId, agentId: filters.agentId };
  const [status, timeseries, models, tools, agents, sessions, llmCalls, errors, subagents, mcpCalls, costTrends, costSummary, costTrends30d] = await Promise.all([
    get("/status"),
    get("/timeseries", { ...common, bucket: filters.bucket }),
    get("/models/stats", common),
    get("/tools/stats", common),
    get("/agents/stats", common),
    get("/sessions", list),
    get("/llm/calls", list),
    get("/errors/stats", common),
    get("/subagents", list),
    get("/mcp/calls", list),
    get("/cost/trends", { ...common, period: "day" }),
    get("/cost/summary", common),
    get("/cost/trends", { ...costCommon30d, period: "day" }),
  ]);
  const data = { status, timeseries, models, tools, agents, sessions, llmCalls, errors, subagents, mcpCalls, costTrends, costSummary, costTrends30d };
  // Patch costs using cached pricing data (computed from token counts)
  patchDashboardCosts(data, getPricing());
  return data;
}

export const loadSession = (sessionId) => get(`/sessions/${encodeURIComponent(sessionId)}`);
