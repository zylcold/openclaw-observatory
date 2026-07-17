import { comboChart, doughnutChart, lineChart, palette, scatterChart, updateChartData, updateDoughnut, updateScatter, hasChart } from "../charts.js";
import { bytes, compact, esc, money, ms, num, shortTime, setShortTimeRange } from "../format.js";
import { KPI_METRICS } from "../config.js";

// Range constants for time-based formatting decisions
const RANGE_MS = { "1h": 3600000, "6h": 21600000, "24h": 86400000, "7d": 604800000, "30d": 2592000000 };
let _rangeMs = Infinity;

export function setRange(rangeKey) {
  _rangeMs = RANGE_MS[rangeKey] || Infinity;
  setShortTimeRange(_rangeMs);
}

const names = {
  overview: "核心指标", resources: "资源趋势", llm_combo: "LLM 请求 · 延迟 · 错误率",
  model_tokens: "各模型 Token 趋势", token_share: "Token 模型占比", tool_share: "工具调用占比",
  scatter: "LLM 延迟－Token 散点",
  agent_compare: "Agent 对比", heatmap: "时间 × Agent 活跃度", sessions: "会话瀑布图",
  errors_cost: "错误聚合与成本", activity: "Subagent / MCP 调用",
  cost_trends: "成本趋势与预算",
};

const empty = (text = "所选时间范围内暂无数据") => "<div class=\"empty\">" + esc(text) + "</div>";
const table = (headers, rows) => "<div class=\"table-wrap\"><table><thead><tr>" + headers.map((h) => "<th>" + h + "</th>").join("") + "</tr></thead><tbody>" + rows.join("") + "</tbody></table></div>";
const chart = (id, tall = false) => "<div class=\"chart" + (tall ? " tall" : "") + "\"><canvas id=\"" + id + "\"></canvas></div>";

// Compute all possible KPI values from data.
// Returns: { id: { value, note, level } }
function computeKPIs(data, config) {
  const agents = data?.agents || [];
  const sum = (key) => agents.reduce((total, row) => total + Number(row[key] || 0), 0);
  const runs = sum("runs");
  const runErrors = sum("runErrors");
  const errorRateVal = runs ? 100 * runErrors / runs : 0;
  const llmReqs = sum("llmRequests");
  const llmErrs = sum("llmErrors");
  const latency = llmReqs ? agents.reduce((v, row) => v + Number(row.llmDurationMs || 0), 0) / llmReqs : 0;
  const disk = [...(data?.timeseries?.points || [])].reverse().find((point) => Number(point.diskTotalBytes || 0) > 0) || {};
  const memPoint = [...(data?.timeseries?.points || [])].reverse().find((point) => Number(point.maxMemoryBytes || 0) > 0) || {};
  const cpuPoint = [...(data?.timeseries?.points || [])].reverse().find((point) => Number(point.averageCpuPercent || 0) > 0) || {};
  const status = data?.status || {};
  const level = (value, warning, critical) => value >= critical ? "critical" : value >= warning ? "warning" : "";
  const t = config.thresholds;
  return {
    runs: { value: compact(runs), note: runErrors + " failed", level: level(errorRateVal, t.errorRateWarning, t.errorRateCritical) },
    llmRequests: { value: compact(llmReqs), note: llmErrs + " LLM errors", level: "" },
    totalTokens: { value: compact(sum("totalTokens")), note: compact(sum("cacheReadTokens")) + " cache read", level: "" },
    toolCalls: { value: compact(sum("toolCalls")), note: sum("toolErrors") + " errors", level: "" },
    avgLlmLatency: { value: ms(latency), note: agents.length + " agents", level: level(latency, t.llmLatencyWarningMs, t.llmLatencyCriticalMs) },
    cost: { value: money(sum("costUsd")), note: "reported cost", level: "" },
    diskUsage: { value: Number(disk.diskUsedPercent || 0).toFixed(1) + "%", note: bytes(disk.diskAvailableBytes) + " available", level: "" },
    inputTokens: { value: compact(sum("inputTokens")), note: "prompt tokens", level: "" },
    outputTokens: { value: compact(sum("outputTokens")), note: "completion tokens", level: "" },
    cacheReadTokens: { value: compact(sum("cacheReadTokens")), note: "cache hits", level: "" },
    cacheWriteTokens: { value: compact(sum("cacheWriteTokens")), note: "cache writes", level: "" },
    llmErrors: { value: num(llmErrs), note: llmReqs ? (100 * llmErrs / llmReqs).toFixed(1) + "% error rate" : "", level: level(llmReqs ? 100 * llmErrs / llmReqs : 0, t.errorRateWarning, t.errorRateCritical) },
    runErrors: { value: num(runErrors), note: runs ? errorRateVal.toFixed(1) + "% error rate" : "", level: level(errorRateVal, t.errorRateWarning, t.errorRateCritical) },
    toolErrors: { value: num(sum("toolErrors")), note: sum("toolCalls") + " total calls", level: "" },
    errorRate: { value: errorRateVal.toFixed(1) + "%", note: runErrors + " of " + runs + " runs", level: level(errorRateVal, t.errorRateWarning, t.errorRateCritical) },
    toolDuration: { value: ms(sum("toolDurationMs")), note: "across all tools", level: "" },
    activeSessions: { value: num(status.sessionsActive || 0), note: "currently active", level: "" },
    agentCount: { value: num(agents.length), note: agents.map((a) => esc(a.agentId)).slice(0, 5).join(", ") + (agents.length > 5 ? "…" : ""), level: "" },
    maxMemory: { value: bytes(memPoint.maxMemoryBytes || 0), note: "peak resident", level: "" },
    avgCpu: { value: Number(cpuPoint.averageCpuPercent || 0).toFixed(1) + "%", note: "avg CPU", level: "" },
  };
}

function overview(data, config, kpiEditorOpen) {
  const kpis = computeKPIs(data, config);
  const kpiNames = new Map(KPI_METRICS);
  const visibleMetrics = (config.kpiMetrics || []).filter((m) => m.visible);
  // Ensure at least the visible ones are shown even if config is stale
  if (!visibleMetrics.length) return "<div class=\"empty\">没有选中的 KPI 指标，点击编辑按钮添加。</div>";
  var cards = visibleMetrics.map((m) => {
    var kpi = kpis[m.id] || { value: "—", note: "", level: "" };
    return "<div class=\"kpi " + kpi.level + "\" data-kpi-id=\"" + m.id + "\"><span>" + esc(kpiNames.get(m.id) || m.id) + "</span><strong>" + kpi.value + "</strong><small>" + esc(kpi.note) + "</small></div>";
  });
  var html = "<div class=\"kpi-grid\" style=\"--kpi-count:" + visibleMetrics.length + "\">" + cards.join("") + "</div>";
  if (kpiEditorOpen) html += kpiEditorHTML(config, kpiNames);
  return html;
}

function kpiEditorHTML(config, kpiNames) {
  const metrics = config.kpiMetrics || [];
  const visibleCount = metrics.filter((m) => m.visible).length;
  var rows = metrics.map((m) => {
    var name = esc(kpiNames.get(m.id) || m.id);
    return '<label class="kpi-check" data-kpi-id="' + m.id + '"><input type="checkbox" data-kpi-visible="' + m.id + '" ' + (m.visible ? 'checked' : '') + '><span class="kpi-drag" title="拖拽排序">⠿</span>' + name + '</label>';
  });
  var hidden = KPI_METRICS.filter(([id]) => !metrics.some((m) => m.id === id));
  if (hidden.length) {
    rows.push('<div class="kpi-divider"></div>');
    hidden.forEach(([id, label]) => {
      rows.push('<label class="kpi-check" data-kpi-id="' + id + '"><input type="checkbox" data-kpi-visible="' + id + '"><span class="kpi-drag" title="拖拽排序">⠿</span>' + esc(label) + '</label>');
    });
  }
  return '<div class="kpi-editor"><div class="kpi-editor-header"><span>指标管理</span><small>' + visibleCount + ' 个展示中 · 点击勾选 / 拖拽排序</small></div><div class="kpi-editor-list" id="kpi-editor-list">' + rows.join('') + '</div></div>';
}

function agentTable(data) {
  if (!data.agents.length) return empty();
  return table(["Agent", "Runs", "Token", "Tools", "Avg Duration", "Error Rate", "Cost"], data.agents.map((a) => "<tr><td><b>" + esc(a.agentId) + "</b></td><td>" + num(a.runs) + "</td><td>" + compact(a.totalTokens) + "</td><td>" + num(a.toolCalls) + "</td><td>" + ms(a.averageDurationMs) + "</td><td>" + Number(a.errorRate || 0).toFixed(1) + "%</td><td>" + money(a.costUsd) + "</td></tr>"));
}

function heatmap(data) {
  const rows = data.timeseries?.agents || [];
  if (!rows.length) return empty();
  var times = [...new Set(rows.map((r) => r.time))];
  const agents = [...new Set(rows.map((r) => r.agentId))];
  var lookup = new Map(rows.map((r) => [r.agentId + "|" + r.time, Number(r.runs || 0)]));
  // Cap at 24 columns — aggregate if more time slots than 24
  if (times.length > 24) {
    const minTime = Math.min(...times.map((t) => Date.parse(t)));
    const maxTime = Math.max(...times.map((t) => Date.parse(t)));
    const span = (maxTime - minTime) || 1;
    const slotWidth = span / 24;
    const slotLabels = [];
    for (let i = 0; i < 24; i++) slotLabels.push(new Date(minTime + (i + 0.5) * slotWidth).toISOString());
    lookup = new Map();
    for (const r of rows) {
      const t = Date.parse(r.time);
      const slotIdx = Math.min(23, Math.max(0, Math.floor((t - minTime) / slotWidth)));
      const key = r.agentId + "|" + slotIdx;
      lookup.set(key, (lookup.get(key) || 0) + Number(r.runs || 0));
    }
    times = slotLabels;
    const max = Math.max(1, ...lookup.values());
    var html = "<div class=\"heatmap\" style=\"--columns:24\"><div></div>";
    for (let i = 0; i < 24; i++) {
      html += "<span class=\"heat-label\">" + (i % 4 === 0 ? esc(shortTime(times[i])) : "") + "</span>";
    }
    agents.forEach((agent) => {
      html += "<b>" + esc(agent) + "</b>";
      for (let i = 0; i < 24; i++) {
        const value = lookup.get(agent + "|" + i) || 0;
        html += "<i title=\"" + esc(agent) + " · " + esc(shortTime(times[i])) + ": " + value + "\" style=\"--heat:" + (value / max) + "\"></i>";
      }
    });
    html += "<div class=\"heat-legend\"><span>0</span><i></i><span>" + compact(max) + "</span></div>";
    html += "</div>";
    return html;
  }
  // Original path: render all time slots as-is (≤24)
  const max = Math.max(1, ...lookup.values());
  var html2 = "<div class=\"heatmap\" style=\"--columns:" + times.length + "\"><div></div>";
  times.forEach((time, i) => {
    html2 += "<span class=\"heat-label\">" + (i % Math.ceil(times.length / 8) === 0 ? esc(shortTime(time)) : "") + "</span>";
  });
  agents.forEach((agent) => {
    html2 += "<b>" + esc(agent) + "</b>";
    times.forEach((time) => {
      const value = lookup.get(agent + "|" + time) || 0;
      html2 += "<i title=\"" + esc(agent) + " · " + esc(shortTime(time)) + ": " + value + "\" style=\"--heat:" + (value / max) + "\"></i>";
    });
  });
  html2 += "<div class=\"heat-legend\"><span>0</span><i></i><span>" + compact(max) + "</span></div>";
  html2 += "</div>";
  return html2;
}

function waterfall(detail) {
  const timeline = detail?.timeline || [];
  if (!timeline.length) return empty("Select a session to see LLM, Tool, MCP and Subagent timeline");
  const starts = timeline.map((x) => Date.parse(x.startedAt)).filter(Number.isFinite);
  const ends = timeline.map((x) => Date.parse(x.endedAt) || Date.parse(x.startedAt) + Number(x.durationMs || 1)).filter(Number.isFinite);
  const min = Math.min(...starts), max = Math.max(...ends, min + 1), span = max - min;
  var html = "<div class=\"waterfall\"><div class=\"waterfall-axis\"><span>0</span><span>" + ms(span / 2) + "</span><span>" + ms(span) + "</span></div>";
  timeline.forEach((item) => {
    const start = Date.parse(item.startedAt);
    const end = Date.parse(item.endedAt) || start + Number(item.durationMs || 1);
    const left = Math.max(0, 100 * (start - min) / span);
    const width = Math.max(0.7, 100 * (end - start) / span);
    const tokens = Number(item.inputTokens || 0) + Number(item.outputTokens || 0) + Number(item.cacheReadTokens || 0) + Number(item.cacheWriteTokens || 0);
    const failCls = item.status === "failed" ? " failed" : "";
    const w = Math.min(width, 100 - left);
    html += "<div class=\"waterfall-row\"><span class=\"wf-label\"><em class=\"kind " + esc(item.kind) + "\">" + esc(item.kind) + "</em>" + esc(item.label) + "</span><div class=\"wf-track\"><i class=\"wf-bar " + esc(item.kind) + failCls + "\" style=\"left:" + left + "%;width:" + w + "%\" title=\"" + esc(item.label) + " · " + ms(end - start) + (tokens ? " · " + compact(tokens) + " token" : "") + "\"></i></div><span>" + ms(end - start) + "</span></div>";
  });
  html += "</div>";
  return html;
}

function sessions(data, detail) {
  const rows = data.sessions || [];
  var selector = "";
  if (rows.length) {
    var opts = rows.map((s) => "<option value=\"" + esc(s.sessionId) + "\"" + (detail?.sessionId === s.sessionId ? " selected" : "") + ">" + esc(s.agentId || "unknown") + " · " + esc(shortTime(s.startedAt)) + " · " + esc(s.status) + "</option>").join("");
    selector = "<label class=\"session-select\">Session <select id=\"session-picker\">" + opts + "</select></label>";
  }
  return selector + waterfall(detail);
}

function errorsCost(data) {
  const errorRows = data.errors.length ? table(["Source", "Category", "Count", "Avg Duration", "Last Seen"], data.errors.map((e) => "<tr><td><span class=\"pill\">" + esc(e.kind) + "</span></td><td>" + esc(e.category) + "</td><td>" + num(e.errors) + "</td><td>" + ms(e.averageDurationMs) + "</td><td>" + esc(shortTime(e.lastOccurredAt)) + "</td></tr>")) : empty("No errors in current range");
  const modelRows = data.models.length ? table(["Model", "Requests", "Token", "Cost"], data.models.map((m) => "<tr><td>" + esc(m.provider) + " / <b>" + esc(m.model) + "</b></td><td>" + num(m.requests) + "</td><td>" + compact(Number(m.inputTokens || 0) + Number(m.outputTokens || 0) + Number(m.cacheReadTokens || 0) + Number(m.cacheWriteTokens || 0)) + "</td><td>" + money(m.costUsd) + "</td></tr>")) : empty("No cost data in current range");
  return "<div class=\"split\"><section><h3>Error Categories</h3>" + errorRows + "</section><section><h3>Model Costs</h3>" + modelRows + "</section></div>";
}

function costTrendsHTML(data, config) {
  const summary = data?.costSummary || {};
  const trends = data?.costTrends || [];
  const totalCost = Number(summary.totalCost || 0);
  const lastDayCost = Number(summary.lastDayCost || 0);
  const lastWeekCost = Number(summary.lastWeekCost || 0);
  const lastMonthCost = Number(summary.lastMonthCost || 0);
  const totalRequests = Number(summary.totalRequests || 0);
  const avgCost = totalRequests ? totalCost / totalRequests : 0;

  // Budget alert
  const budget = config?.thresholds?.costBudgetUsd || 0;
  const budgetUsed = budget > 0 ? Math.min(100, 100 * lastMonthCost / budget) : 0;
  var budgetAlert = "";
  if (budget > 0 && budgetUsed >= 100) budgetAlert = "critical";
  else if (budget > 0 && budgetUsed >= 80) budgetAlert = "warning";

  var budgetHTML;
  if (budget > 0) {
    budgetHTML = "<div class=\"kpi " + budgetAlert + "\"><span>Monthly Budget</span><strong>" + money(budget) + "</strong><small>" + budgetUsed.toFixed(0) + "% used · " + money(Math.max(0, budget - lastMonthCost)) + " left</small></div>";
  } else {
    budgetHTML = "<div class=\"kpi\"><span>Monthly Budget</span><strong>Not Set</strong><small>Set costBudgetUsd in config</small></div>";
  }

  var kpiItems = [
    { label: "Total Cost", value: money(totalCost), note: num(totalRequests) + " requests" },
    { label: "Today", value: money(lastDayCost), note: avgCost ? "avg " + money(avgCost) + "/req" : "" },
    { label: "This Week", value: money(lastWeekCost), note: "" },
    { label: "This Month", value: money(lastMonthCost), note: "" },
  ];
  var kpiHTML = "<div class=\"kpi-grid\">";
  kpiItems.forEach((k) => { kpiHTML += "<div class=\"kpi\"><span>" + k.label + "</span><strong>" + k.value + "</strong><small>" + k.note + "</small></div>"; });
  kpiHTML += budgetHTML + "</div>";

  // Cost trend chart
  var chartHTML = chart("cost-trend-chart", true);

  // Cost by model table from trends
  var modelRows = {};
  trends.forEach((row) => {
    var key = row.provider + "/" + row.model;
    if (!modelRows[key]) modelRows[key] = { provider: row.provider, model: row.model, cost: 0, requests: 0 };
    modelRows[key].cost += Number(row.costUsd || 0);
    modelRows[key].requests += Number(row.requests || 0);
  });
  var modelList = Object.values(modelRows).sort((a, b) => b.cost - a.cost);
  var modelTable = modelList.length
    ? table(["Model", "Requests", "Cost", "Avg/Req"], modelList.map((m) => "<tr><td>" + esc(m.provider) + " / <b>" + esc(m.model) + "</b></td><td>" + num(m.requests) + "</td><td>" + money(m.cost) + "</td><td>" + money(m.requests ? m.cost / m.requests : 0) + "</td></tr>"))
    : empty("No cost data in selected range");

  return kpiHTML + chartHTML + "<section><h3>Cost by Model</h3>" + modelTable + "</section>";
}

function activity(data) {
  const sub = data.subagents.length ? table(["Subagent", "Agent", "Model", "Status", "Started"], data.subagents.slice(0, 20).map((x) => "<tr><td>" + esc(x.subagentId) + "</td><td>" + esc(x.agentId) + "</td><td>" + esc(x.model || "—") + "</td><td><span class=\"status " + esc(x.status) + "\">" + esc(x.status) + "</span></td><td>" + esc(shortTime(x.startedAt)) + "</td></tr>")) : empty("No subagent runs in current range");
  const mcp = data.mcpCalls.length ? table(["MCP Tool", "Agent", "Owner", "Duration", "Status"], data.mcpCalls.slice(0, 20).map((x) => "<tr><td><b>" + esc(x.toolName || "unknown") + "</b></td><td>" + esc(x.agentId) + "</td><td>" + esc(x.owner || "—") + "</td><td>" + ms(x.durationMs) + "</td><td><span class=\"status " + esc(x.status) + "\">" + esc(x.status) + "</span></td></tr>")) : empty("No MCP calls in current range");
  return "<div class=\"split\"><section><h3>Subagent Runs</h3>" + sub + "</section><section><h3>MCP Calls</h3>" + mcp + "</section></div>";
}

function toolRanking(tools) {
  if (!tools.length) return empty("No tool calls in current range");
  const max = Math.max(...tools.map((tool) => Number(tool.calls || 0)), 1);
  return "<div class=\"tool-ranking\">" + tools.map((tool) => "<div title=\"" + esc(tool.source) + ":" + esc(tool.tool) + " · " + num(tool.calls) + "\"><span>" + esc(tool.source) + ":" + esc(tool.tool) + "</span><i><b style=\"width:" + (100 * Number(tool.calls || 0) / max) + "%\"></b></i><strong>" + num(tool.calls) + "</strong></div>").join("") + "</div>";
}

export function moduleHTML(id, data, config, sessionDetail, kpiEditorOpen) {
  var body = "";
  if (id === "overview") body = overview(data, config, kpiEditorOpen);
  if (id === "resources") body = chart("resources-chart");
  if (id === "llm_combo") body = chart("llm-combo-chart");
  if (id === "model_tokens") body = chart("model-token-chart", true);
  if (id === "token_share") body = chart("token-share-chart");
  if (id === "tool_share") body = chart("tool-share-chart") + "<div class=\"tool-ranking-wrap\">" + toolRanking(data.tools) + "</div>";
  if (id === "scatter") body = chart("scatter-chart");
  if (id === "agent_compare") body = chart("agent-chart") + agentTable(data);
  if (id === "heatmap") body = heatmap(data);
  if (id === "sessions") body = sessions(data, sessionDetail);
  if (id === "errors_cost") body = errorsCost(data);
  if (id === "activity") body = activity(data);
  if (id === "cost_trends") body = costTrendsHTML(data, config);
  var editBtn = id === "overview"
    ? "<button class=\"kpi-edit-btn" + (kpiEditorOpen ? " active" : "") + "\" id=\"kpi-edit-toggle\" title=\"编辑指标\">" + (kpiEditorOpen ? "✓" : "✎") + "</button>"
    : "";
  return "<article class=\"panel module-" + id + "\" draggable=\"true\" data-module=\"" + id + "\"><header><div><span class=\"drag\" title=\"Drag to reorder\">⠿</span><h2>" + names[id] + "</h2></div>" + editBtn + "</header>" + body + "</article>";
}

export function paintCharts(data) {
  const points = data.timeseries?.points || [];
  const labels = points.map((p) => shortTime(p.time));
  lineChart("resources-chart", labels, [
    { label: "Memory MiB", data: points.map((p) => Number(p.averageMemoryBytes || 0) / 1048576), borderColor: palette[0], backgroundColor: "rgba(38,214,162,.14)", fill: true, yAxisID: "y" },
    { label: "CPU %", data: points.map((p) => p.averageCpuPercent || 0), borderColor: palette[1], backgroundColor: "rgba(91,157,255,.12)", fill: true, yAxisID: "y1" },
    { label: "Disk %", data: points.map((p) => p.diskUsedPercent || 0), borderColor: palette[2], backgroundColor: "transparent", fill: false, pointRadius: 0, yAxisID: "y1" },
  ], { scales: { y: { beginAtZero: true, position: "left" }, y1: { beginAtZero: true, position: "right", grid: { drawOnChartArea: false } } } });

  comboChart("llm-combo-chart", labels, [
    { type: "bar", label: "LLM Requests", data: points.map((p) => p.llmRequests || 0), backgroundColor: "rgba(38,214,162,.45)", yAxisID: "y" },
    { type: "line", label: "Avg Latency ms", data: points.map((p) => p.averageLlmDurationMs || 0), borderColor: palette[1], pointRadius: 1, yAxisID: "y1" },
    { type: "line", label: "Error Rate %", data: points.map((p) => p.llmErrorRate || 0), borderColor: palette[4], pointRadius: 1, yAxisID: "y2" },
  ], { scales: { y: { beginAtZero: true }, y1: { beginAtZero: true, position: "right", grid: { drawOnChartArea: false } }, y2: { display: false, beginAtZero: true } } });

  const modelRows = data.timeseries?.models || [];
  const modelKeys = [...new Set(modelRows.map((r) => r.provider + "/" + r.model))];
  const timeKeys = [...new Set(modelRows.map((r) => r.time))];
  const byModelTime = new Map(modelRows.map((r) => [r.provider + "/" + r.model + "|" + r.time, Number(r.inputTokens || 0) + Number(r.outputTokens || 0) + Number(r.cacheReadTokens || 0) + Number(r.cacheWriteTokens || 0)]));
  lineChart("model-token-chart", timeKeys.map(shortTime), modelKeys.map((key, i) => ({ label: key, data: timeKeys.map((time) => byModelTime.get(key + "|" + time) || 0), borderColor: palette[i % palette.length], backgroundColor: palette[i % palette.length] + "33", fill: true, stack: "tokens", pointRadius: 0 })), { scales: { y: { beginAtZero: true, stacked: true }, x: { stacked: true } } });

  doughnutChart("token-share-chart", data.models.map((m) => m.provider + "/" + m.model), data.models.map((m) => Number(m.inputTokens || 0) + Number(m.outputTokens || 0) + Number(m.cacheReadTokens || 0) + Number(m.cacheWriteTokens || 0)));
  const topTools = data.tools.slice(0, 7);
  const otherToolCalls = data.tools.slice(7).reduce((total, tool) => total + Number(tool.calls || 0), 0);
  var toolLabels = topTools.map((tool) => tool.source + ":" + tool.tool);
  var toolValues = topTools.map((tool) => tool.calls);
  if (otherToolCalls > 0) { toolLabels.push("Other"); toolValues.push(otherToolCalls); }
  doughnutChart("tool-share-chart", toolLabels, toolValues);

  var scatterGroups = new Map();
  data.llmCalls.forEach((call) => {
    var key = (call.provider || "unknown") + "/" + (call.model || "unknown");
    if (!scatterGroups.has(key)) scatterGroups.set(key, []);
    scatterGroups.get(key).push({ x: Number(call.totalTokens || 0), y: Number(call.durationMs || 0), call });
  });
  scatterChart("scatter-chart", [...scatterGroups].map((entry, i) => ({ label: entry[0], data: entry[1], backgroundColor: palette[i % palette.length], pointRadius: 4 })));

  comboChart("agent-chart", data.agents.map((a) => a.agentId), [
    { type: "bar", label: "Runs", data: data.agents.map((a) => a.runs), backgroundColor: "rgba(38,214,162,.5)", yAxisID: "y" },
    { type: "line", label: "Token", data: data.agents.map((a) => a.totalTokens), borderColor: palette[1], yAxisID: "y1" },
    { type: "line", label: "Error %", data: data.agents.map((a) => a.errorRate), borderColor: palette[4], yAxisID: "y2" },
  ], { scales: { y: { beginAtZero: true }, y1: { position: "right", beginAtZero: true, grid: { drawOnChartArea: false } }, y2: { display: false } } });

  // Cost trend chart
  paintCostTrendChart(data);
}

function paintCostTrendChart(data) {
  var trends = data?.costTrends || [];
  if (!trends.length) return;
  var periods = [...new Set(trends.map((r) => r.period))].sort();
  var modelKeys = [...new Set(trends.map((r) => r.provider + "/" + r.model))];
  var byModelPeriod = new Map(trends.map((r) => [r.provider + "/" + r.model + "|" + r.period, Number(r.costUsd || 0)]));
  comboChart("cost-trend-chart", periods, modelKeys.map((key, i) => ({
    type: "bar",
    label: key,
    data: periods.map((p) => byModelPeriod.get(key + "|" + p) || 0),
    backgroundColor: palette[i % palette.length] + "88",
    stack: "cost",
  })), { scales: { y: { beginAtZero: true, stacked: true }, x: { stacked: true } } });
}

/**
 * Incrementally update existing charts without destroying them.
 * Returns true if all charts were updated in-place, false if a full re-render is needed.
 */
export function updateCharts(data) {
  var points = data.timeseries?.points || [];
  var labels = points.map((p) => shortTime(p.time));

  var requiredCharts = ["resources-chart", "llm-combo-chart", "model-token-chart", "token-share-chart", "tool-share-chart", "scatter-chart", "agent-chart"];
  var allExist = requiredCharts.every((id) => hasChart(id));
  if (!allExist) return false;

  updateChartData("resources-chart", labels, [
    { data: points.map((p) => Number(p.averageMemoryBytes || 0) / 1048576) },
    { data: points.map((p) => p.averageCpuPercent || 0) },
    { data: points.map((p) => p.diskUsedPercent || 0) },
  ]);

  updateChartData("llm-combo-chart", labels, [
    { data: points.map((p) => p.llmRequests || 0) },
    { data: points.map((p) => p.averageLlmDurationMs || 0) },
    { data: points.map((p) => p.llmErrorRate || 0) },
  ]);

  var modelRows = data.timeseries?.models || [];
  var modelKeys = [...new Set(modelRows.map((r) => r.provider + "/" + r.model))];
  var timeKeys = [...new Set(modelRows.map((r) => r.time))];
  var byModelTime = new Map(modelRows.map((r) => [r.provider + "/" + r.model + "|" + r.time, Number(r.inputTokens || 0) + Number(r.outputTokens || 0) + Number(r.cacheReadTokens || 0) + Number(r.cacheWriteTokens || 0)]));
  var modelDatasets = modelKeys.map((key, i) => ({ label: key, data: timeKeys.map((time) => byModelTime.get(key + "|" + time) || 0), borderColor: palette[i % palette.length], backgroundColor: palette[i % palette.length] + "33", fill: true, stack: "tokens", pointRadius: 0 }));
  updateChartData("model-token-chart", timeKeys.map(shortTime), modelDatasets);

  updateDoughnut("token-share-chart",
    data.models.map((m) => m.provider + "/" + m.model),
    data.models.map((m) => Number(m.inputTokens || 0) + Number(m.outputTokens || 0) + Number(m.cacheReadTokens || 0) + Number(m.cacheWriteTokens || 0)));

  var topTools = data.tools.slice(0, 7);
  var otherToolCalls = data.tools.slice(7).reduce((total, tool) => total + Number(tool.calls || 0), 0);
  var toolLabels = topTools.map((tool) => tool.source + ":" + tool.tool);
  var toolValues = topTools.map((tool) => tool.calls);
  if (otherToolCalls > 0) { toolLabels.push("Other"); toolValues.push(otherToolCalls); }
  updateDoughnut("tool-share-chart", toolLabels, toolValues);

  var scatterGroups = new Map();
  data.llmCalls.forEach((call) => {
    var key = (call.provider || "unknown") + "/" + (call.model || "unknown");
    if (!scatterGroups.has(key)) scatterGroups.set(key, []);
    scatterGroups.get(key).push({ x: Number(call.totalTokens || 0), y: Number(call.durationMs || 0), call });
  });
  var scatterDatasets = [...scatterGroups].map((entry, i) => ({ label: entry[0], data: entry[1], backgroundColor: palette[i % palette.length], pointRadius: 4 }));
  updateScatter("scatter-chart", scatterDatasets);

  updateChartData("agent-chart", data.agents.map((a) => a.agentId), [
    { data: data.agents.map((a) => a.runs) },
    { data: data.agents.map((a) => a.totalTokens) },
    { data: data.agents.map((a) => a.errorRate) },
  ]);

  // Cost trend chart (optional — may not exist if no data)
  if (hasChart("cost-trend-chart")) {
    var trends = data?.costTrends || [];
    var periods = [...new Set(trends.map((r) => r.period))].sort();
    var costModelKeys = [...new Set(trends.map((r) => r.provider + "/" + r.model))];
    var byModelPeriod = new Map(trends.map((r) => [r.provider + "/" + r.model + "|" + r.period, Number(r.costUsd || 0)]));
    updateChartData("cost-trend-chart", periods, costModelKeys.map((key) => ({
      data: periods.map((p) => byModelPeriod.get(key + "|" + p) || 0),
      label: key,
    })));
  }

  return true;
}

/**
 * Update KPI cards and tables in-place without full re-render.
 */
export function updateNonChartDOM(appEl, data, config) {
  var kpis = computeKPIs(data, config);
  var kpiNames = new Map(KPI_METRICS);
  var cards = appEl.querySelectorAll(".kpi[data-kpi-id]");
  cards.forEach((card) => {
    var id = card.dataset.kpiId;
    var kpi = kpis[id];
    if (!kpi) return;
    var strong = card.querySelector("strong");
    var small = card.querySelector("small");
    if (strong) strong.textContent = kpi.value;
    if (small) small.textContent = kpi.note;
    card.className = "kpi " + (kpi.level || "");
  });
}

/**
 * Update the agent comparison table in-place.
 */
export function updateAgentTable(appEl, data) {
  var tbody = appEl.querySelector(".module-agent_compare tbody");
  if (!tbody || !data.agents.length) return;
  tbody.innerHTML = data.agents.map((a) => "<tr><td><b>" + esc(a.agentId) + "</b></td><td>" + num(a.runs) + "</td><td>" + compact(a.totalTokens) + "</td><td>" + num(a.toolCalls) + "</td><td>" + ms(a.averageDurationMs) + "</td><td>" + Number(a.errorRate || 0).toFixed(1) + "%</td><td>" + money(a.costUsd) + "</td></tr>").join("");
}
