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
];

export const DEFAULT_CONFIG = {
  version: 2,
  theme: "dark",
  refreshInterval: 15000,
  modules: MODULES.map(([id]) => ({ id, visible: true })),
  kpiMetrics: KPI_METRICS.map(([id, , vis]) => ({ id, visible: vis })),
  customCharts: normalizeCustomCharts(DEFAULT_CUSTOM_CHARTS),
  thresholds: { errorRateWarning: 5, errorRateCritical: 15, llmLatencyWarningMs: 5000, llmLatencyCriticalMs: 15000, costBudgetUsd: 0 },
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
  const interval = Number(input.refreshInterval);
  const hasCustomCharts = Object.prototype.hasOwnProperty.call(input, "customCharts");
  return {
    version: 2,
    theme: input.theme === "light" ? "light" : "dark",
    refreshInterval: [5000, 15000, 30000, 60000, 0].includes(interval) ? interval : DEFAULT_CONFIG.refreshInterval,
    modules,
    kpiMetrics,
    customCharts: normalizeCustomCharts(input.customCharts, { useDefaults: !hasCustomCharts }),
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
