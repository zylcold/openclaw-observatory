const API = "/api/v1";

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
  const [status, timeseries, models, tools, agents, sessions, llmCalls, errors, subagents, mcpCalls] = await Promise.all([
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
  ]);
  return { status, timeseries, models, tools, agents, sessions, llmCalls, errors, subagents, mcpCalls };
}

export const loadSession = (sessionId) => get(`/sessions/${encodeURIComponent(sessionId)}`);
