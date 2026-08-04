import { DEFAULT_CUSTOM_CHARTS, normalizeCustomCharts } from "./custom-chart-model.js";

export const MODULES = [
  ["overview", "核心指标"],
  ["sessions", "会话瀑布图"],
  ["errors_cost", "错误聚合与成本"],
  ["activity", "Subagent / MCP 调用"],
];

// All available KPI metrics for the overview module.
// id: unique key, label: display name, defaultVisible: shown by default.
export const KPI_METRICS = [
  ["runs", "Agent Runs", true],
  ["llmRequests", "LLM Requests", true],
  ["totalTokens", "Token Usage", true],
  ["toolCalls", "Tool + MCP Calls", true],
  ["avgLlmLatency", "Avg LLM Latency", true],
  ["cost", "Total Cost", true],
  ["diskUsage", "Disk Usage", true],
  ["inputTokens", "Input Tokens", false],
  ["outputTokens", "Output Tokens", false],
  ["cacheReadTokens", "Cache Read Tokens", false],
  ["cacheWriteTokens", "Cache Write Tokens", false],
  ["llmErrors", "LLM Errors", false],
  ["runErrors", "Run Errors", false],
  ["toolErrors", "Tool Errors", false],
  ["errorRate", "Error Rate", false],
  ["toolDuration", "Tool Total Duration", false],
  ["activeSessions", "Active Sessions", false],
  ["agentCount", "Agent Count", false],
  ["maxMemory", "Peak Memory", false],
  ["avgCpu", "Avg CPU %", false],
  ["lifetimeTokens", "Token 总量(累计)", false],
  ["lifetimeCost", "成本总量(累计)", false],
];

// Per-section KPI definitions (for sections other than overview, which uses KPI_METRICS).
export const SECTION_KPIS = {
  overview: [
    ["onlineAgents", "在线 Agent", true],
    ["activeSessions", "活跃 Session", true],
    ["runs", "任务数", true],
    ["successRate", "成功率", true],
    ["totalTokens", "Token", true],
    ["cost", "Cost", true],
    ["errors", "异常数", true],
    ["llmRequests", "LLM 调用", true],
    ["avgLatency", "平均延迟", true],
    ["toolCalls", "Tool 调用", true],
    ["cacheRate", "Cache 命中率", true],
    ["daemonHealth", "Daemon 健康", true],
    ["dataFlow", "数据流", true],
    ["inputTokens", "Input Token", false],
    ["outputTokens", "Output Token", false],
    ["errorRate", "错误率", false],
    ["diskUsage", "磁盘用量", false],
    ["memoryUsage", "内存用量", false],
  ],
  cost_trends: [
    ["totalCost", "Total Cost", true],
    ["today", "Today", true],
    ["thisWeek", "This Week", true],
    ["thisMonth", "This Month", true],
    ["monthlyBudget", "Monthly Budget", true],
    ["avgCostPerReq", "Avg Cost/Request", false],
  ],
};

export const DEFAULT_CONFIG = {
  version: 3,
  theme: "dark",
  refreshInterval: 15000,
  modules: MODULES.map(([id]) => ({ id, visible: true })),
  kpiMetrics: KPI_METRICS.map(([id, , vis]) => ({ id, visible: vis })),
  sectionKpis: Object.fromEntries(
    Object.entries(SECTION_KPIS).map(([sectionId, kpis]) => [
      sectionId,
      kpis.map(([id, , vis]) => ({ id, visible: vis })),
    ])
  ),
  customCharts: normalizeCustomCharts(DEFAULT_CUSTOM_CHARTS),
  thresholds: {
    errorRateWarning: 5, errorRateCritical: 15,
    llmLatencyWarningMs: 5000, llmLatencyCriticalMs: 15000,
    toolP99WarningMs: 5000, toolP99CriticalMs: 15000,
    sessionStuckMs: 3600000, costBudgetUsd: 0,
  },
};

const KEY = "openclaw-observatory-dashboard-v3";
const copy = (value) => JSON.parse(JSON.stringify(value));

export function normalizeConfig(input = {}) {
  const known = new Map(MODULES);
  const seen = new Set();
  const modules = [];
  for (const item of Array.isArray(input.modules) ? input.modules : []) {
    if (known.has(item?.id) && !seen.has(item.id)) {
      modules.push({ id: item.id, visible: item.visible !== false });
      seen.add(item.id);
    }
  }
  for (const [id] of MODULES) if (!seen.has(id)) modules.push({ id, visible: true });
  // Normalize KPI metrics
  const knownKpis = new Map(KPI_METRICS);
  const seenKpis = new Set();
  const kpiMetrics = [];
  for (const item of Array.isArray(input.kpiMetrics) ? input.kpiMetrics : []) {
    if (knownKpis.has(item?.id) && !seenKpis.has(item.id)) {
      kpiMetrics.push({ id: item.id, visible: item.visible !== false });
      seenKpis.add(item.id);
    }
  }
  for (const [id, , vis] of KPI_METRICS) if (!seenKpis.has(id)) kpiMetrics.push({ id, visible: vis });
  // Normalize section KPIs
  const sectionKpis = {};
  for (const [sectionId, defs] of Object.entries(SECTION_KPIS)) {
    const inputSection = input.sectionKpis?.[sectionId];
    const known = new Map(defs);
    const seen = new Set();
    const list = [];
    for (const item of Array.isArray(inputSection) ? inputSection : []) {
      if (known.has(item?.id) && !seen.has(item.id)) {
        list.push({ id: item.id, visible: item.visible !== false });
        seen.add(item.id);
      }
    }
    for (const [id, , vis] of defs) if (!seen.has(id)) list.push({ id, visible: vis });
    sectionKpis[sectionId] = list;
  }
  const interval = Number(input.refreshInterval);
  const hasCustomCharts = Object.prototype.hasOwnProperty.call(input, "customCharts");
  let customCharts = input.customCharts;
  if (hasCustomCharts && Number(input.version) === 2) {
    const existing = Array.isArray(customCharts) ? customCharts : [];
    const existingIds = new Set(existing.map((item) => item?.id));
    customCharts = [...existing, ...DEFAULT_CUSTOM_CHARTS.filter((item) => !existingIds.has(item.id))];
  }
  return {
    version: 3,
    theme: input.theme === "light" ? "light" : "dark",
    refreshInterval: [5000, 15000, 30000, 60000, 0].includes(interval) ? interval : DEFAULT_CONFIG.refreshInterval,
    modules,
    kpiMetrics,
    sectionKpis,
    customCharts: normalizeCustomCharts(customCharts, { useDefaults: !hasCustomCharts }),
    thresholds: { ...DEFAULT_CONFIG.thresholds, ...(input.thresholds || {}) },
  };
}

export function loadConfig() {
  try { return normalizeConfig(JSON.parse(localStorage.getItem(KEY) || "{}")); }
  catch { return copy(DEFAULT_CONFIG); }
}

export function saveConfig(config) {
  const value = normalizeConfig(config);
  localStorage.setItem(KEY, JSON.stringify(value));
  return value;
}

export function resetConfig() {
  localStorage.removeItem(KEY);
  return copy(DEFAULT_CONFIG);
}
