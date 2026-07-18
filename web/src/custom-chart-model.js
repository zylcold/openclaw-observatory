const number = (value) => Number(value || 0);
const tokenTotal = (row) => number(row.inputTokens)
  + number(row.outputTokens)
  + number(row.cacheReadTokens)
  + number(row.cacheWriteTokens);
const metric = (id, label, value, unit = "") => ({ id, label, value, unit });
const dimension = (id, label, value) => ({ id, label, value });

export const CHART_TYPES = [
  { id: "line", label: "折线图", description: "观察连续变化与趋势", datasets: ["overview", "agents", "models"] },
  { id: "area", label: "面积图", description: "突出趋势与累计量级", datasets: ["overview", "agents", "models"] },
  { id: "bar", label: "柱状图", description: "比较多个分类", datasets: ["overview", "agents", "models", "tools", "errors"] },
  { id: "horizontalBar", label: "横向条形图", description: "适合较长分类名称", datasets: ["agents", "models", "tools", "errors"] },
  { id: "doughnut", label: "环形图", description: "查看构成与占比", datasets: ["agents", "models", "tools", "errors"] },
  { id: "pie", label: "饼图", description: "展示分类分布", datasets: ["agents", "models", "tools", "errors"] },
  { id: "polarArea", label: "极区图", description: "对比分类规模", datasets: ["agents", "models", "tools", "errors"] },
  { id: "radar", label: "雷达图", description: "查看多分类轮廓", datasets: ["agents", "models", "tools", "errors"] },
];

export const DIMENSION_GROUPS = [
  {
    id: "overview",
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
    ],
  },
  {
    id: "models",
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
      metric("costUsd", "成本", (row) => row.costUsd, "USD"),
    ],
  },
  {
    id: "tools",
    label: "工具调用",
    description: "按来源与工具名称拆分",
    dimensions: [
      dimension("source", "来源", (row) => row.source || "tool"),
      dimension("tool", "工具", (row) => row.tool || "unknown"),
    ],
    rows: (data) => data?.tools || [],
    metrics: [
      metric("calls", "调用次数", (row) => row.calls),
      metric("errors", "错误次数", (row) => row.errors),
      metric("averageDurationMs", "平均耗时", (row) => row.averageDurationMs, "ms"),
      metric("maxDurationMs", "最大耗时", (row) => row.maxDurationMs, "ms"),
    ],
  },
  {
    id: "errors",
    label: "错误聚合",
    description: "按来源与错误类别拆分",
    dimensions: [
      dimension("kind", "来源", (row) => row.kind || "unknown"),
      dimension("category", "错误类别", (row) => row.category || "unknown"),
    ],
    rows: (data) => data?.errors || [],
    metrics: [
      metric("errors", "错误次数", (row) => row.errors),
      metric("averageDurationMs", "平均耗时", (row) => row.averageDurationMs, "ms"),
    ],
  },
];

export const DEFAULT_CUSTOM_CHARTS = [
  { id: "default-resource", title: "资源内存趋势", chartType: "area", dataset: "overview", dimensions: ["time"], metric: "averageMemoryMiB", width: "half" },
  { id: "default-llm-requests", title: "LLM 请求趋势", chartType: "bar", dataset: "overview", dimensions: ["time"], metric: "llmRequests", width: "half" },
  { id: "default-model-tokens", title: "各模型 Token 趋势", chartType: "area", dataset: "models", dimensions: ["time", "model"], metric: "totalTokens", width: "full" },
  { id: "default-token-share", title: "Token 模型占比", chartType: "doughnut", dataset: "models", dimensions: ["model"], metric: "totalTokens", width: "half" },
  { id: "default-tools", title: "工具调用排行", chartType: "horizontalBar", dataset: "tools", dimensions: ["tool"], metric: "calls", width: "half" },
  { id: "default-agents", title: "Agent Runs 对比", chartType: "bar", dataset: "agents", dimensions: ["agent"], metric: "runs", width: "full" },
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
