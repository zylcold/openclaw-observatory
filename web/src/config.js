export const MODULES = [
  ["overview", "核心指标"],
  ["resources", "资源趋势"],
  ["llm_combo", "LLM 请求 · 延迟 · 错误率"],
  ["model_tokens", "各模型 Token 趋势"],
  ["shares", "Token / 工具占比"],
  ["scatter", "LLM 延迟－Token 散点"],
  ["agent_compare", "Agent 对比"],
  ["heatmap", "时间 × Agent 活跃度"],
  ["sessions", "会话瀑布图"],
  ["errors_cost", "错误聚合与成本"],
  ["activity", "Subagent / MCP 调用"],
  ["cost_trends", "成本趋势与预算"],
];

export const DEFAULT_CONFIG = {
  version: 1,
  theme: "dark",
  refreshInterval: 15000,
  modules: MODULES.map(([id]) => ({ id, visible: true })),
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
  const interval = Number(input.refreshInterval);
  return {
    version: 1,
    theme: input.theme === "light" ? "light" : "dark",
    refreshInterval: [5000, 15000, 30000, 60000, 0].includes(interval) ? interval : DEFAULT_CONFIG.refreshInterval,
    modules,
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
