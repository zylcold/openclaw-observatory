import { comboChart, doughnutChart, lineChart, palette, scatterChart } from "../charts.js";
import { bytes, compact, esc, money, ms, num, shortTime } from "../format.js";

const names = {
  overview: "核心指标", resources: "资源趋势", llm_combo: "LLM 请求 · 延迟 · 错误率",
  model_tokens: "各模型 Token 趋势", shares: "Token / 工具占比", scatter: "LLM 延迟－Token 散点",
  agent_compare: "Agent 对比", heatmap: "时间 × Agent 活跃度", sessions: "会话瀑布图",
  errors_cost: "错误聚合与成本", activity: "Subagent / MCP 调用",
};

const empty = (text = "所选时间范围内暂无数据") => `<div class="empty">${esc(text)}</div>`;
const table = (headers, rows) => `<div class="table-wrap"><table><thead><tr>${headers.map((h) => `<th>${h}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table></div>`;
const chart = (id, tall = false) => `<div class="chart${tall ? " tall" : ""}"><canvas id="${id}"></canvas></div>`;

function overview(data, config) {
  const agents = data?.agents || [];
  const sum = (key) => agents.reduce((total, row) => total + Number(row[key] || 0), 0);
  const runs = sum("runs");
  const errors = sum("runErrors");
  const errorRate = runs ? 100 * errors / runs : 0;
  const latency = sum("llmRequests") ? agents.reduce((v, row) => v + Number(row.llmDurationMs || 0), 0) / sum("llmRequests") : 0;
  const disk = [...(data?.timeseries?.points || [])].reverse().find((point) => Number(point.diskTotalBytes || 0) > 0) || {};
  const level = (value, warning, critical) => value >= critical ? "critical" : value >= warning ? "warning" : "";
  const t = config.thresholds;
  const items = [
    ["Agent Runs", compact(runs), `${errors} failed`, level(errorRate, t.errorRateWarning, t.errorRateCritical)],
    ["LLM Requests", compact(sum("llmRequests")), `${errorRate.toFixed(1)}% run errors`, level(errorRate, t.errorRateWarning, t.errorRateCritical)],
    ["Token", compact(sum("totalTokens")), `${compact(sum("cacheReadTokens"))} cache read`, ""],
    ["Tool + MCP", compact(sum("toolCalls")), `${sum("toolErrors")} errors`, ""],
    ["平均 LLM 延迟", ms(latency), `${agents.length} agents`, level(latency, t.llmLatencyWarningMs, t.llmLatencyCriticalMs)],
    ["Cost", money(sum("costUsd")), "reported cost", ""],
    ["磁盘使用率", `${Number(disk.diskUsedPercent || 0).toFixed(1)}%`, `${bytes(disk.diskAvailableBytes)} 可用`, ""],
  ];
  return `<div class="kpi-grid">${items.map(([label, value, note, state]) => `<div class="kpi ${state}"><span>${label}</span><strong>${value}</strong><small>${note}</small></div>`).join("")}</div>`;
}

function agentTable(data) {
  if (!data.agents.length) return empty();
  return table(["Agent", "Runs", "Token", "Tools", "平均耗时", "错误率", "Cost"], data.agents.map((a) => `<tr><td><b>${esc(a.agentId)}</b></td><td>${num(a.runs)}</td><td>${compact(a.totalTokens)}</td><td>${num(a.toolCalls)}</td><td>${ms(a.averageDurationMs)}</td><td>${Number(a.errorRate || 0).toFixed(1)}%</td><td>${money(a.costUsd)}</td></tr>`));
}

function heatmap(data) {
  const rows = data.timeseries?.agents || [];
  if (!rows.length) return empty();
  const times = [...new Set(rows.map((r) => r.time))];
  const agents = [...new Set(rows.map((r) => r.agentId))];
  const lookup = new Map(rows.map((r) => [`${r.agentId}|${r.time}`, Number(r.runs || 0)]));
  const max = Math.max(1, ...lookup.values());
  return `<div class="heatmap" style="--columns:${times.length}"><div></div>${times.map((time, i) => `<span class="heat-label">${i % Math.ceil(times.length / 8) === 0 ? esc(shortTime(time)) : ""}</span>`).join("")}${agents.map((agent) => `<b>${esc(agent)}</b>${times.map((time) => { const value = lookup.get(`${agent}|${time}`) || 0; return `<i title="${esc(agent)} · ${esc(shortTime(time))}: ${value}" style="--heat:${value / max}"></i>`; }).join("")}`).join("")}</div>`;
}

function waterfall(detail) {
  const timeline = detail?.timeline || [];
  if (!timeline.length) return empty("选择一条会话查看 LLM、Tool、MCP 与 Subagent 时间线");
  const starts = timeline.map((x) => Date.parse(x.startedAt)).filter(Number.isFinite);
  const ends = timeline.map((x) => Date.parse(x.endedAt) || Date.parse(x.startedAt) + Number(x.durationMs || 1)).filter(Number.isFinite);
  const min = Math.min(...starts), max = Math.max(...ends, min + 1), span = max - min;
  return `<div class="waterfall"><div class="waterfall-axis"><span>0</span><span>${ms(span / 2)}</span><span>${ms(span)}</span></div>${timeline.map((item) => {
    const start = Date.parse(item.startedAt); const end = Date.parse(item.endedAt) || start + Number(item.durationMs || 1);
    const left = Math.max(0, 100 * (start - min) / span); const width = Math.max(0.7, 100 * (end - start) / span);
    const tokens = Number(item.inputTokens || 0) + Number(item.outputTokens || 0) + Number(item.cacheReadTokens || 0) + Number(item.cacheWriteTokens || 0);
    return `<div class="waterfall-row"><span class="wf-label"><em class="kind ${esc(item.kind)}">${esc(item.kind)}</em>${esc(item.label)}</span><div class="wf-track"><i class="wf-bar ${esc(item.kind)} ${item.status === "failed" ? "failed" : ""}" style="left:${left}%;width:${Math.min(width, 100 - left)}%" title="${esc(item.label)} · ${ms(end - start)}${tokens ? ` · ${compact(tokens)} token` : ""}"></i></div><span>${ms(end - start)}</span></div>`;
  }).join("")}</div>`;
}

function sessions(data, detail) {
  const rows = data.sessions || [];
  const selector = rows.length ? `<label class="session-select">会话 <select id="session-picker">${rows.map((s) => `<option value="${esc(s.sessionId)}" ${detail?.sessionId === s.sessionId ? "selected" : ""}>${esc(s.agentId || "unknown")} · ${esc(shortTime(s.startedAt))} · ${esc(s.status)}</option>`).join("")}</select></label>` : "";
  return `${selector}${waterfall(detail)}`;
}

function errorsCost(data) {
  const errorRows = data.errors.length ? table(["来源", "Category", "次数", "平均耗时", "最近发生"], data.errors.map((e) => `<tr><td><span class="pill">${esc(e.kind)}</span></td><td>${esc(e.category)}</td><td>${num(e.errors)}</td><td>${ms(e.averageDurationMs)}</td><td>${esc(shortTime(e.lastOccurredAt))}</td></tr>`)) : empty("当前范围没有错误");
  const modelRows = data.models.length ? table(["模型", "请求", "Token", "Cost"], data.models.map((m) => `<tr><td>${esc(m.provider)} / <b>${esc(m.model)}</b></td><td>${num(m.requests)}</td><td>${compact(Number(m.inputTokens || 0) + Number(m.outputTokens || 0) + Number(m.cacheReadTokens || 0) + Number(m.cacheWriteTokens || 0))}</td><td>${money(m.costUsd)}</td></tr>`)) : empty("当前范围没有成本数据");
  return `<div class="split"><section><h3>错误分类</h3>${errorRows}</section><section><h3>模型成本</h3>${modelRows}</section></div>`;
}

function activity(data) {
  const sub = data.subagents.length ? table(["Subagent", "Agent", "模型", "状态", "开始"], data.subagents.slice(0, 20).map((x) => `<tr><td>${esc(x.subagentId)}</td><td>${esc(x.agentId)}</td><td>${esc(x.model || "—")}</td><td><span class="status ${esc(x.status)}">${esc(x.status)}</span></td><td>${esc(shortTime(x.startedAt))}</td></tr>`)) : empty("当前范围没有 Subagent 运行");
  const mcp = data.mcpCalls.length ? table(["MCP Tool", "Agent", "Owner", "耗时", "状态"], data.mcpCalls.slice(0, 20).map((x) => `<tr><td><b>${esc(x.toolName || "unknown")}</b></td><td>${esc(x.agentId)}</td><td>${esc(x.owner || "—")}</td><td>${ms(x.durationMs)}</td><td><span class="status ${esc(x.status)}">${esc(x.status)}</span></td></tr>`)) : empty("当前范围没有 MCP 调用");
  return `<div class="split"><section><h3>Subagent Runs</h3>${sub}</section><section><h3>MCP Calls</h3>${mcp}</section></div>`;
}

function toolRanking(tools) {
  if (!tools.length) return empty("当前范围没有工具调用");
  const max = Math.max(...tools.map((tool) => Number(tool.calls || 0)), 1);
  return `<div class="tool-ranking">${tools.map((tool) => `<div title="${esc(tool.source)}:${esc(tool.tool)} · ${num(tool.calls)} 次"><span>${esc(tool.source)}:${esc(tool.tool)}</span><i><b style="width:${100 * Number(tool.calls || 0) / max}%"></b></i><strong>${num(tool.calls)}</strong></div>`).join("")}</div>`;
}

export function moduleHTML(id, data, config, sessionDetail) {
  let body = "";
  if (id === "overview") body = overview(data, config);
  if (id === "resources") body = chart("resources-chart");
  if (id === "llm_combo") body = chart("llm-combo-chart");
  if (id === "model_tokens") body = chart("model-token-chart", true);
  if (id === "shares") body = `<div class="split charts"><section><h3>Token 按模型</h3>${chart("token-share-chart")}</section><section><h3>工具调用 · Top 7 + 完整排行</h3>${chart("tool-share-chart")}${toolRanking(data.tools)}</section></div>`;
  if (id === "scatter") body = chart("scatter-chart");
  if (id === "agent_compare") body = `${chart("agent-chart")}${agentTable(data)}`;
  if (id === "heatmap") body = heatmap(data);
  if (id === "sessions") body = sessions(data, sessionDetail);
  if (id === "errors_cost") body = errorsCost(data);
  if (id === "activity") body = activity(data);
  return `<article class="panel module-${id}" draggable="true" data-module="${id}"><header><div><span class="drag" title="拖拽排序">⠿</span><h2>${names[id]}</h2></div><small>${id === "overview" ? "阈值自动高亮" : ""}</small></header>${body}</article>`;
}

export function paintCharts(data) {
  const points = data.timeseries?.points || [];
  const labels = points.map((p) => shortTime(p.time));
  lineChart("resources-chart", labels, [
    { label: "内存 MiB", data: points.map((p) => Number(p.averageMemoryBytes || 0) / 1048576), borderColor: palette[0], backgroundColor: "rgba(38,214,162,.14)", fill: true, yAxisID: "y" },
    { label: "CPU %", data: points.map((p) => p.averageCpuPercent || 0), borderColor: palette[1], backgroundColor: "rgba(91,157,255,.12)", fill: true, yAxisID: "y1" },
    { label: "磁盘使用率 %", data: points.map((p) => p.diskUsedPercent || 0), borderColor: palette[2], backgroundColor: "transparent", fill: false, pointRadius: 0, yAxisID: "y1" },
  ], { scales: { y: { beginAtZero: true, position: "left" }, y1: { beginAtZero: true, position: "right", grid: { drawOnChartArea: false } } } });

  comboChart("llm-combo-chart", labels, [
    { type: "bar", label: "LLM 请求", data: points.map((p) => p.llmRequests || 0), backgroundColor: "rgba(38,214,162,.45)", yAxisID: "y" },
    { type: "line", label: "平均延迟 ms", data: points.map((p) => p.averageLlmDurationMs || 0), borderColor: palette[1], pointRadius: 1, yAxisID: "y1" },
    { type: "line", label: "错误率 %", data: points.map((p) => p.llmErrorRate || 0), borderColor: palette[4], pointRadius: 1, yAxisID: "y2" },
  ], { scales: { y: { beginAtZero: true }, y1: { beginAtZero: true, position: "right", grid: { drawOnChartArea: false } }, y2: { display: false, beginAtZero: true } } });

  const modelRows = data.timeseries?.models || [];
  const modelKeys = [...new Set(modelRows.map((r) => `${r.provider}/${r.model}`))];
  const timeKeys = [...new Set(modelRows.map((r) => r.time))];
  const byModelTime = new Map(modelRows.map((r) => [`${r.provider}/${r.model}|${r.time}`, Number(r.inputTokens || 0) + Number(r.outputTokens || 0) + Number(r.cacheReadTokens || 0) + Number(r.cacheWriteTokens || 0)]));
  lineChart("model-token-chart", timeKeys.map(shortTime), modelKeys.map((key, i) => ({ label: key, data: timeKeys.map((time) => byModelTime.get(`${key}|${time}`) || 0), borderColor: palette[i % palette.length], backgroundColor: `${palette[i % palette.length]}33`, fill: true, stack: "tokens", pointRadius: 0 })), { scales: { y: { beginAtZero: true, stacked: true }, x: { stacked: true } } });

  doughnutChart("token-share-chart", data.models.map((m) => `${m.provider}/${m.model}`), data.models.map((m) => Number(m.inputTokens || 0) + Number(m.outputTokens || 0) + Number(m.cacheReadTokens || 0) + Number(m.cacheWriteTokens || 0)));
  const topTools = data.tools.slice(0, 7);
  const otherToolCalls = data.tools.slice(7).reduce((total, tool) => total + Number(tool.calls || 0), 0);
  const toolLabels = topTools.map((tool) => `${tool.source}:${tool.tool}`);
  const toolValues = topTools.map((tool) => tool.calls);
  if (otherToolCalls > 0) { toolLabels.push("其他"); toolValues.push(otherToolCalls); }
  doughnutChart("tool-share-chart", toolLabels, toolValues);

  const scatterGroups = new Map();
  data.llmCalls.forEach((call) => {
    const key = `${call.provider || "unknown"}/${call.model || "unknown"}`;
    if (!scatterGroups.has(key)) scatterGroups.set(key, []);
    scatterGroups.get(key).push({ x: Number(call.totalTokens || 0), y: Number(call.durationMs || 0), call });
  });
  scatterChart("scatter-chart", [...scatterGroups].map(([label, values], i) => ({ label, data: values, backgroundColor: palette[i % palette.length], pointRadius: 4 })));

  comboChart("agent-chart", data.agents.map((a) => a.agentId), [
    { type: "bar", label: "Runs", data: data.agents.map((a) => a.runs), backgroundColor: "rgba(38,214,162,.5)", yAxisID: "y" },
    { type: "line", label: "Token", data: data.agents.map((a) => a.totalTokens), borderColor: palette[1], yAxisID: "y1" },
    { type: "line", label: "错误率 %", data: data.agents.map((a) => a.errorRate), borderColor: palette[4], yAxisID: "y2" },
  ], { scales: { y: { beginAtZero: true }, y1: { position: "right", beginAtZero: true, grid: { drawOnChartArea: false } }, y2: { display: false } } });
}
