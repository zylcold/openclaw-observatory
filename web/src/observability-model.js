export const OBSERVABILITY_DOMAINS = [
  {
    id: "overview",
    label: "Overview",
    name: "全局概览",
    glyph: "◎",
    description: "运行规模、健康度、Token 与成本总览",
    phase: "核心",
  },
  {
    id: "agents",
    label: "Agent",
    name: "Agent 监控",
    glyph: "A",
    description: "状态、活跃度、成功率与资源消耗",
    phase: "P1",
  },
  {
    id: "sessions",
    label: "Session",
    name: "Session 与 Trace",
    glyph: "S",
    description: "会话质量、耗时分布与完整调用链",
    phase: "P1",
  },
  {
    id: "models",
    label: "Model",
    name: "模型与成本",
    glyph: "M",
    description: "Token 构成、成本、延迟与调用质量",
    phase: "P1",
  },
  {
    id: "tools",
    label: "Tool / MCP",
    name: "Tool / MCP 性能",
    glyph: "T",
    description: "调用量、失败率与尾延迟定位",
    phase: "P1",
  },
  {
    id: "infrastructure",
    label: "Infrastructure",
    name: "基础设施",
    glyph: "I",
    description: "关键资源摘要与 Grafana 深度监控入口",
    phase: "复用",
  },
  {
    id: "errors",
    label: "Errors",
    name: "异常中心",
    glyph: "!",
    description: "Agent、LLM、Tool 与系统异常聚合",
    phase: "诊断",
  },
];

const domainMap = new Map(OBSERVABILITY_DOMAINS.map((domain) => [domain.id, domain]));

export function observabilityDomain(id) {
  return domainMap.get(id) || domainMap.get("overview");
}

export function normalizeDomain(id) {
  return domainMap.has(id) ? id : "overview";
}

export function chartsForDomain(charts, domain) {
  return (charts || []).filter((chart) => chart.domain === domain);
}

export function favoriteCharts(charts) {
  return (charts || []).filter((chart) => chart.domain !== "overview" && chart.favorite === true);
}

export function chartsForView(charts, domain) {
  const nativeCharts = chartsForDomain(charts, domain);
  return domain === "overview" ? [...nativeCharts, ...favoriteCharts(charts)] : nativeCharts;
}

export function domainFilterOptions(data) {
  return {
    models: [...new Set((data?.models || []).map((row) => row.model).filter(Boolean))].sort(),
    tools: [...new Set((data?.tools || []).map((row) => row.tool).filter(Boolean))].sort(),
    statuses: ["active", "completed", "failed"],
  };
}

export function filterDashboardData(data, filters = {}) {
  if (!data) return data;
  const model = filters.model || "";
  const tool = filters.tool || "";
  const status = filters.status || "";
  const matchesStatus = (row) => !status || row?.status === status;
  const next = {
    ...data,
    sessions: (data.sessions || []).filter(matchesStatus),
    llmCalls: (data.llmCalls || []).filter((row) => (!model || row.model === model) && matchesStatus(row)),
    subagents: (data.subagents || []).filter((row) => (!model || row.model === model) && matchesStatus(row)),
    mcpCalls: (data.mcpCalls || []).filter((row) => (!tool || row.toolName === tool) && matchesStatus(row)),
    models: (data.models || []).filter((row) => !model || row.model === model),
    tools: (data.tools || []).filter((row) => !tool || row.tool === tool),
    anomalies: (data.anomalies || []).filter((row) =>
      (!model || row.kind !== "llm" || row.label === model)
      && (!tool || !["tool", "mcp"].includes(row.kind) || row.label === tool)),
    timeseries: {
      ...(data.timeseries || {}),
      models: (data.timeseries?.models || []).filter((row) => !model || row.model === model),
      tools: (data.timeseries?.tools || []).filter((row) => !tool || row.tool === tool),
    },
  };
  return next;
}

export function percentile(values, ratio) {
  const sorted = values.map(Number).filter(Number.isFinite).sort((a, b) => a - b);
  if (!sorted.length) return 0;
  const index = Math.max(0, Math.ceil(sorted.length * ratio) - 1);
  return sorted[Math.min(index, sorted.length - 1)];
}

export function sessionDuration(session, now = Date.now()) {
  const started = Date.parse(session?.startedAt);
  if (!Number.isFinite(started)) return 0;
  const ended = Date.parse(session?.endedAt);
  const end = Number.isFinite(ended) ? ended : now;
  return Math.max(0, end - started);
}

export function sessionSummary(sessions, now = Date.now()) {
  const rows = sessions || [];
  const durations = rows.map((row) => sessionDuration(row, now));
  return {
    total: rows.length,
    active: rows.filter((row) => row.status === "active").length,
    messages: rows.reduce((sum, row) => sum + Number(row.messageCount || 0), 0),
    p50: percentile(durations, 0.5),
    p90: percentile(durations, 0.9),
    p99: percentile(durations, 0.99),
  };
}

export function agentStatus(agent, sessions) {
  const related = (sessions || []).filter((session) => session.agentId === agent.agentId);
  const active = related.filter((session) => session.status === "active").length;
  const failures = Number(agent.runErrors || 0);
  return {
    state: active ? "active" : failures ? "degraded" : "idle",
    activeSessions: active,
    sessions: related.length,
  };
}
