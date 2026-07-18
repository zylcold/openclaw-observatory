const number = (value) => Number(value || 0);
const tokenTotal = (row) => number(row.inputTokens)
  + number(row.outputTokens)
  + number(row.cacheReadTokens)
  + number(row.cacheWriteTokens);
const metric = (id, label, value, unit = "") => ({ id, label, value, unit });
const dimension = (id, label, value) => ({ id, label, value });

export const CHART_TYPES = [
  { id: "line", label: "折线图", description: "观察连续变化与趋势", datasets: ["overview", "agents", "sessions", "models", "tools", "infrastructure", "errors"] },
  { id: "area", label: "面积图", description: "突出趋势与累计量级", datasets: ["overview", "agents", "sessions", "models", "tools", "infrastructure", "errors"] },
  { id: "bar", label: "柱状图", description: "比较多个分类", datasets: ["overview", "agents", "sessions", "models", "tools", "infrastructure", "errors"] },
  { id: "horizontalBar", label: "横向条形图", description: "适合较长分类名称", datasets: ["agents", "sessions", "models", "tools", "errors"] },
  { id: "doughnut", label: "环形图", description: "查看构成与占比", datasets: ["agents", "sessions", "models", "tools", "errors"] },
  { id: "pie", label: "饼图", description: "展示分类分布", datasets: ["agents", "sessions", "models", "tools", "errors"] },
  { id: "polarArea", label: "极区图", description: "对比分类规模", datasets: ["agents", "sessions", "models", "tools", "errors"] },
  { id: "radar", label: "雷达图", description: "查看多分类轮廓", datasets: ["agents", "sessions", "models", "tools", "errors"] },
];

export const DIMENSION_GROUPS = [
  {
    id: "overview",
    domain: "overview",
    label: "运行趋势",
    description: "系统、LLM 与资源的时间序列",
    dimensions: [dimension("time", "时间", (row) => row.time)],
    rows: (data) => data?.timeseries?.points || [],
    metrics: [
      metric("runs", "Agent Runs", (row) => row.runs),
      metric("runErrors", "Run 错误", (row) => row.runErrors),
      metric("llmRequests", "LLM 请求", (row) => row.llmRequests),
      metric("llmErrorRate", "LLM 错误率", (row) => row.llmErrorRate, "%"),
      metric("totalTokens", "Token 总量", tokenTotal),
      metric("costUsd", "成本", (row) => row.costUsd, "USD"),
      metric("averageLlmDurationMs", "LLM 平均延迟", (row) => row.averageLlmDurationMs, "ms"),
      metric("toolCalls", "工具调用", (row) => row.toolCalls),
      metric("toolErrors", "工具错误", (row) => row.toolErrors),
      metric("averageMemoryMiB", "平均内存", (row) => number(row.averageMemoryBytes) / 1048576, "MiB"),
      metric("averageCpuPercent", "平均 CPU", (row) => row.averageCpuPercent, "%"),
      metric("diskUsedPercent", "磁盘使用率", (row) => row.diskUsedPercent, "%"),
    ],
  },
  {
    id: "agents",
    domain: "agents",
    label: "Agent 运行",
    description: "按时间与 Agent 拆分运行指标",
    dimensions: [
      dimension("time", "时间", (row) => row.time),
      dimension("agent", "Agent", (row) => row.agentId || "unknown"),
    ],
    rows: (data, dimensions) => dimensions.includes("time") ? data?.timeseries?.agents || [] : data?.agents || [],
    metrics: [
      metric("runs", "Runs", (row) => row.runs),
      metric("errors", "错误数", (row) => row.errors ?? row.runErrors),
      metric("durationMs", "总耗时", (row) => row.durationMs ?? row.totalDurationMs, "ms"),
      metric("successRate", "成功率", (row) => row.successRate ?? (100 - number(row.errorRate)), "%"),
      metric("totalTokens", "Token 总量", (row) => row.totalTokens),
      metric("costUsd", "成本", (row) => row.costUsd, "USD"),
      metric("toolCalls", "Tool / MCP 调用", (row) => row.toolCalls),
    ],
  },
  {
    id: "sessions",
    domain: "sessions",
    label: "Session",
    description: "按时间、Agent 与状态拆分会话",
    dimensions: [
      dimension("time", "开始时间", (row) => row.startedAt),
      dimension("agent", "Agent", (row) => row.agentId || "unknown"),
      dimension("status", "状态", (row) => row.status || "unknown"),
    ],
    rows: (data) => data?.sessions || [],
    metrics: [
      metric("sessions", "Session 数", () => 1),
      metric("messages", "Message 数", (row) => row.messageCount),
      metric("durationMs", "持续时间", (row) => {
        const start = Date.parse(row.startedAt);
        const end = Date.parse(row.endedAt);
        return Number.isFinite(start) && Number.isFinite(end) ? Math.max(0, end - start) : 0;
      }, "ms"),
    ],
  },
  {
    id: "models",
    domain: "models",
    label: "模型调用",
    description: "按时间、Provider 与 Model 拆分",
    dimensions: [
      dimension("time", "时间", (row) => row.time),
      dimension("provider", "Provider", (row) => row.provider || "unknown"),
      dimension("model", "Model", (row) => row.model || "unknown"),
    ],
    rows: (data, dimensions) => dimensions.includes("time") ? data?.timeseries?.models || [] : data?.models || [],
    metrics: [
      metric("requests", "请求数", (row) => row.requests),
      metric("totalTokens", "Token 总量", tokenTotal),
      metric("inputTokens", "输入 Token", (row) => row.inputTokens),
      metric("outputTokens", "输出 Token", (row) => row.outputTokens),
      metric("cacheReadTokens", "缓存读取 Token", (row) => row.cacheReadTokens),
      metric("cacheWriteTokens", "缓存写入 Token", (row) => row.cacheWriteTokens),
      metric("costUsd", "成本", (row) => row.costUsd, "USD"),
      metric("averageDurationMs", "平均延迟", (row) => row.averageDurationMs, "ms"),
      metric("errorRate", "错误率", (row) => row.errorRate ?? (number(row.requests) ? 100 * number(row.errors) / number(row.requests) : 0), "%"),
    ],
  },
  {
    id: "tools",
    domain: "tools",
    label: "工具调用",
    description: "按来源与工具名称拆分",
    dimensions: [
      dimension("time", "时间", (row) => row.time),
      dimension("source", "来源", (row) => row.source || "tool"),
      dimension("tool", "工具", (row) => row.tool || "unknown"),
      dimension("agent", "Agent", (row) => row.agentId || "unknown"),
    ],
    rows: (data, dimensions) => dimensions.includes("time") || dimensions.includes("agent")
      ? data?.timeseries?.tools || []
      : data?.tools || [],
    metrics: [
      metric("calls", "调用次数", (row) => row.calls),
      metric("errors", "错误次数", (row) => row.errors),
      metric("failureRate", "失败率", (row) => row.failureRate ?? (number(row.calls) ? 100 * number(row.errors) / number(row.calls) : 0), "%"),
      metric("averageDurationMs", "平均耗时", (row) => row.averageDurationMs, "ms"),
      metric("p95DurationMs", "P95", (row) => row.p95DurationMs, "ms"),
      metric("p99DurationMs", "P99", (row) => row.p99DurationMs, "ms"),
      metric("maxDurationMs", "最大耗时", (row) => row.maxDurationMs, "ms"),
      metric("timeouts", "Timeout", (row) => row.timeouts),
    ],
  },
  {
    id: "infrastructure",
    domain: "infrastructure",
    label: "基础设施",
    description: "Observatory 采集的宿主机关键资源摘要",
    dimensions: [dimension("time", "时间", (row) => row.time)],
    rows: (data) => data?.timeseries?.points || [],
    metrics: [
      metric("averageMemoryMiB", "平均内存", (row) => number(row.averageMemoryBytes) / 1048576, "MiB"),
      metric("maxMemoryMiB", "峰值内存", (row) => number(row.maxMemoryBytes) / 1048576, "MiB"),
      metric("averageCpuPercent", "平均 CPU", (row) => row.averageCpuPercent, "%"),
      metric("diskUsedPercent", "磁盘使用率", (row) => row.diskUsedPercent, "%"),
    ],
  },
  {
    id: "errors",
    domain: "errors",
    label: "错误聚合",
    description: "按来源与错误类别拆分",
    dimensions: [
      dimension("time", "时间", (row) => row.time),
      dimension("kind", "来源", (row) => row.kind || "unknown"),
      dimension("category", "错误类别", (row) => row.category || "unknown"),
    ],
    rows: (data, dimensions) => {
      if (!dimensions.includes("time")) return data?.errors || [];
      return (data?.timeseries?.points || []).flatMap((row) => [
        { ...row, kind: "Agent", errors: number(row.runErrors) },
        { ...row, kind: "LLM", errors: number(row.llmErrors) },
        { ...row, kind: "Tool / MCP", errors: number(row.toolErrors) },
      ]);
    },
    metrics: [
      metric("errors", "错误次数", (row) => row.errors),
      metric("averageDurationMs", "平均耗时", (row) => row.averageDurationMs, "ms"),
    ],
  },
];

export const DEFAULT_CUSTOM_CHARTS = [
  { id: "default-task-trend", title: "任务趋势", chartType: "line", dataset: "overview", dimensions: ["time"], metric: "runs", width: "half" },
  { id: "default-token-trend", title: "Token 趋势", chartType: "area", dataset: "overview", dimensions: ["time"], metric: "totalTokens", width: "half" },
  { id: "default-cost-trend", title: "成本趋势", chartType: "bar", dataset: "overview", dimensions: ["time"], metric: "costUsd", width: "half" },
  { id: "default-status-share", title: "Session 状态分布", chartType: "doughnut", dataset: "sessions", dimensions: ["status"], metric: "sessions", width: "half", domain: "overview" },
  { id: "default-agent-activity", title: "Agent 活跃趋势", chartType: "area", dataset: "agents", dimensions: ["time", "agent"], metric: "runs", width: "full" },
  { id: "default-agent-token", title: "Agent Token 排行", chartType: "horizontalBar", dataset: "agents", dimensions: ["agent"], metric: "totalTokens", width: "half" },
  { id: "default-agent-success", title: "Agent 成功率", chartType: "bar", dataset: "agents", dimensions: ["agent"], metric: "successRate", width: "half" },
  { id: "default-session-trend", title: "Session 趋势", chartType: "line", dataset: "sessions", dimensions: ["time", "status"], metric: "sessions", width: "full" },
  { id: "default-session-duration", title: "Session 耗时对比", chartType: "bar", dataset: "sessions", dimensions: ["agent"], metric: "durationMs", width: "half" },
  { id: "default-session-messages", title: "Message 分布", chartType: "doughnut", dataset: "sessions", dimensions: ["agent"], metric: "messages", width: "half" },
  { id: "default-model-tokens", title: "各模型 Token 趋势", chartType: "area", dataset: "models", dimensions: ["time", "model"], metric: "totalTokens", width: "full" },
  { id: "default-model-cost", title: "模型成本排行", chartType: "horizontalBar", dataset: "models", dimensions: ["model"], metric: "costUsd", width: "half" },
  { id: "default-model-latency", title: "模型延迟排行", chartType: "bar", dataset: "models", dimensions: ["model"], metric: "averageDurationMs", width: "half" },
  { id: "default-tool-trend", title: "Tool / MCP 调用趋势", chartType: "line", dataset: "tools", dimensions: ["time", "tool"], metric: "calls", width: "full" },
  { id: "default-tools", title: "调用量 Top N", chartType: "horizontalBar", dataset: "tools", dimensions: ["tool"], metric: "calls", width: "half" },
  { id: "default-tool-latency", title: "P95 耗时 Top N", chartType: "bar", dataset: "tools", dimensions: ["tool"], metric: "p95DurationMs", width: "half" },
  { id: "default-infra-resource", title: "内存趋势", chartType: "area", dataset: "infrastructure", dimensions: ["time"], metric: "averageMemoryMiB", width: "half" },
  { id: "default-infra-cpu", title: "CPU 趋势", chartType: "line", dataset: "infrastructure", dimensions: ["time"], metric: "averageCpuPercent", width: "half" },
  { id: "default-infra-disk", title: "磁盘使用率", chartType: "line", dataset: "infrastructure", dimensions: ["time"], metric: "diskUsedPercent", width: "full" },
  { id: "default-error-trend", title: "错误趋势", chartType: "area", dataset: "errors", dimensions: ["time", "kind"], metric: "errors", width: "full" },
  { id: "default-error-share", title: "错误类型分布", chartType: "doughnut", dataset: "errors", dimensions: ["kind"], metric: "errors", width: "half" },
  { id: "default-error-category", title: "异常类别排行", chartType: "horizontalBar", dataset: "errors", dimensions: ["category"], metric: "errors", width: "half" },
];

const chartTypeMap = new Map(CHART_TYPES.map((item) => [item.id, item]));
const groupMap = new Map(DIMENSION_GROUPS.map((item) => [item.id, item]));

export function chartTypeById(id) {
  return chartTypeMap.get(id);
}

export function dimensionGroupById(id) {
  return groupMap.get(id);
}

export function dimensionById(groupId, dimensionId) {
  return groupMap.get(groupId)?.dimensions.find((item) => item.id === dimensionId);
}

export function metricById(groupId, metricId) {
  return groupMap.get(groupId)?.metrics.find((item) => item.id === metricId);
}

export function suitableDimensionGroups(chartTypeId) {
  const allowed = new Set(chartTypeById(chartTypeId)?.datasets || []);
  return DIMENSION_GROUPS.filter((group) => allowed.has(group.id));
}

export function suitableDimensionGroupsForDomain(chartTypeId, domain) {
  return suitableDimensionGroups(chartTypeId).filter((group) => group.domain === domain);
}

export function defaultCustomChartTitle(groupId, dimensionIds, metricId) {
  const group = dimensionGroupById(groupId);
  const selectedMetric = metricById(groupId, metricId);
  const labels = (dimensionIds || []).map((id) => dimensionById(groupId, id)?.label).filter(Boolean);
  return `${selectedMetric?.label || "指标"} · ${labels.join(" × ") || group?.label || "维度"}`;
}

export function normalizeCustomCharts(input, { useDefaults = false } = {}) {
  const source = Array.isArray(input) ? input : (useDefaults ? DEFAULT_CUSTOM_CHARTS : []);
  const seen = new Set();
  const result = [];
  for (const [index, item] of source.entries()) {
    const chartType = chartTypeById(item?.chartType);
    const group = dimensionGroupById(item?.dataset);
    if (!chartType || !group || !chartType.datasets.includes(group.id)) continue;
    const dimensions = [...new Set(Array.isArray(item.dimensions) ? item.dimensions : [item.dimension])]
      .filter((id) => dimensionById(group.id, id))
      .slice(0, 2);
    if (!dimensions.length || !metricById(group.id, item.metric)) continue;
    const fallbackId = `chart-${index + 1}`;
    let id = String(item.id || fallbackId).replace(/[^a-zA-Z0-9_-]/g, "-").slice(0, 64) || fallbackId;
    while (seen.has(id)) id = `${id}-${index + 1}`;
    seen.add(id);
    const fallbackTitle = defaultCustomChartTitle(group.id, dimensions, item.metric);
    const title = String(item.title || fallbackTitle).trim().slice(0, 80);
    result.push({
      id,
      title: title || fallbackTitle,
      chartType: chartType.id,
      dataset: group.id,
      dimensions,
      metric: item.metric,
      width: item.width === "full" ? "full" : "half",
      domain: String(item.domain || group.domain || "overview"),
    });
  }
  return result;
}

function formatCategory(id, value) {
  if (id !== "time") return String(value);
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? String(value)
    : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

export function buildCustomChartSeries(data, chart) {
  const group = dimensionGroupById(chart?.dataset);
  const selectedMetric = metricById(chart?.dataset, chart?.metric);
  const dimensions = (chart?.dimensions || []).map((id) => dimensionById(chart.dataset, id)).filter(Boolean);
  if (!group || !selectedMetric || !dimensions.length) return { labels: [], datasets: [], label: "", unit: "" };
  const categoryDimension = dimensions[0];
  const seriesDimension = dimensions[1];
  const rows = group.rows(data, dimensions.map((item) => item.id));
  const categoryValues = new Map();
  const totals = new Map();

  for (const row of rows) {
    const category = String(categoryDimension.value(row));
    const series = seriesDimension ? String(seriesDimension.value(row)) : selectedMetric.label;
    const value = number(selectedMetric.value(row));
    if (!categoryValues.has(category)) categoryValues.set(category, new Map());
    const values = categoryValues.get(category);
    values.set(series, number(values.get(series)) + value);
    totals.set(category, number(totals.get(category)) + value);
  }

  let categories = [...categoryValues.keys()];
  if (categoryDimension.id === "time") categories.sort((a, b) => Date.parse(a) - Date.parse(b));
  else categories.sort((a, b) => number(totals.get(b)) - number(totals.get(a)));
  categories = categories.slice(0, 12);

  let seriesNames = seriesDimension
    ? [...new Set(categories.flatMap((category) => [...categoryValues.get(category).keys()]))]
    : [selectedMetric.label];
  seriesNames.sort((a, b) => {
    const total = (name) => categories.reduce((sum, category) => sum + number(categoryValues.get(category).get(name)), 0);
    return total(b) - total(a);
  });
  seriesNames = seriesNames.slice(0, 8);

  return {
    labels: categories.map((category) => formatCategory(categoryDimension.id, category)),
    datasets: seriesNames.map((name) => ({
      label: name,
      data: categories.map((category) => number(categoryValues.get(category).get(name))),
    })),
    label: selectedMetric.label,
    unit: selectedMetric.unit,
  };
}
