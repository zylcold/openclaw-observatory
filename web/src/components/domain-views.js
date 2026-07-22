import { bytes, compact, esc, money, ms, num, shortTime } from "../format.js";
import { agentStatus, percentile, sessionDuration, sessionSummary } from "../observability-model.js";
import { moduleHTML, sectionKpiEditorHTML } from "./modules.js";

const empty = (text = "当前筛选范围内暂无数据") => `<div class="empty">${esc(text)}</div>`;
const table = (headers, rows) => `<div class="table-wrap"><table><thead><tr>${headers.map((item) => `<th>${item}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table></div>`;

function panel(id, title, body, meta = "", className = "", editBtn = "") {
  return `<article class="panel domain-panel ${className}" data-module="${esc(id)}">
    <header><div><h2>${esc(title)}</h2>${meta ? `<span class="panel-meta">${esc(meta)}</span>` : ""}</div>${editBtn}</header>
    ${body}
  </article>`;
}

function metricStrip(items) {
  return `<div class="domain-metrics" style="--metric-count:${items.length}">${items.map((item) => `
    <div class="domain-metric ${item.level || ""}">
      <span>${esc(item.label)}</span><strong>${item.value}</strong><small>${esc(item.note || "")}</small>
    </div>`).join("")}</div>`;
}

function sum(rows, key) {
  return (rows || []).reduce((total, row) => total + Number(row[key] || 0), 0);
}

function completedRuns(agents) {
  return (agents || []).some((row) => Object.prototype.hasOwnProperty.call(row, "completedRuns"))
    ? sum(agents, "completedRuns")
    : Math.max(0, sum(agents, "runs") - sum(agents, "runErrors") - sum(agents, "activeRuns"));
}

function overviewSummary(data, config, sectionKpiEditor) {
  const agents = data.agents || [];
  const sessions = data.sessions || [];
  const runs = sum(agents, "runs");
  const errors = sum(agents, "runErrors") + sum(agents, "llmErrors") + sum(agents, "toolErrors");
  const onlineAgents = new Set(sessions.filter((row) => row.status === "active").map((row) => row.agentId).filter(Boolean)).size;
  const successRate = runs ? 100 * completedRuns(agents) / runs : 100;
  const llmReqs = sum(agents, "llmRequests");
  const llmDuration = sum(agents, "llmDurationMs");
  const avgLatency = llmReqs ? llmDuration / llmReqs : 0;
  const toolCallsTotal = sum(agents, "toolCalls");
  const inputTok = sum(agents, "inputTokens");
  const outputTok = sum(agents, "outputTokens");
  const cacheRead = sum(agents, "cacheReadTokens");
  const totalTok = sum(agents, "totalTokens");
  const cacheRate = totalTok ? (100 * cacheRead / totalTok).toFixed(1) : "0.0";
  const errorRateVal = runs ? (100 * (sum(agents, "runErrors")) / runs).toFixed(1) : "0.0";
  const point = [...(data.timeseries?.points || [])].reverse().find((row) => Number(row.diskTotalBytes || 0) > 0) || {};
  const memPoint = [...(data.timeseries?.points || [])].reverse().find((row) => Number(row.maxMemoryBytes || 0) > 0) || {};

  // Daemon health & data flow indicators
  const status = data.status || {};
  const daemon = status.daemon || {};
  const lastEventMs = status.lastEventReceivedAt ? Date.parse(status.lastEventReceivedAt) : 0;
  const eventAgeMin = lastEventMs ? Math.round((Date.now() - lastEventMs) / 60000) : null;
  const daemonHealthy = daemon.ready !== false && (eventAgeMin === null || eventAgeMin < 10);
  const recentPts = (data.timeseries?.points || []).slice(-5);
  const hasResource = recentPts.some((p) => Number(p.averageCpuPercent || 0) > 0);
  const hasActivity = recentPts.some((p) => Number(p.runs || 0) > 0 || Number(p.llmRequests || 0) > 0);
  const flowOk = hasActivity || !hasResource; // ok if either has activity or no resource samples yet
  const inst = (status.instances || [])[0] || {};
  const instOk = inst.status === "up";
  const allMetrics = {
    onlineAgents: { label: "在线 Agent", value: num(onlineAgents), note: `${agents.length} 个有观测数据` },
    activeSessions: { label: "活跃 Session", value: num(sessions.filter((row) => row.status === "active").length), note: `${sessions.length} 个会话` },
    runs: { label: "任务数", value: compact(runs), note: `${sum(agents, "runErrors")} 个失败` },
    successRate: { label: "成功率", value: `${successRate.toFixed(1)}%`, note: "Agent Run" },
    totalTokens: { label: "Token", value: compact(totalTok), note: `${compact(cacheRead)} cache read` },
    cost: { label: "Cost", value: money(sum(agents, "costUsd")), note: "所选时间范围" },
    errors: { label: "异常数", value: compact(errors), note: "Agent + LLM + Tool", level: errors ? "critical" : "" },
    llmRequests: { label: "LLM 调用", value: compact(llmReqs), note: `${sum(agents, "llmErrors")} LLM errors` },
    avgLatency: { label: "平均延迟", value: ms(avgLatency), note: "端到端 LLM" },
    toolCalls: { label: "Tool 调用", value: compact(toolCallsTotal), note: `${sum(agents, "toolErrors")} errors` },
    cacheRate: { label: "Cache 命中率", value: cacheRate + "%", note: `${compact(cacheRead)} / ${compact(totalTok)}` },
    daemonHealth: { label: "Daemon 健康", value: daemonHealthy ? "✓ Healthy" : "⚠ 异常", note: daemon.version ? `v${daemon.version}` : "unknown" + (eventAgeMin != null ? ` · ${eventAgeMin}min ago` : ""), level: daemonHealthy ? "" : "critical" },
    dataFlow: { label: "数据流", value: flowOk ? "✓ 正常" : "⚠ 中断", note: instOk ? `instance up` : "instance down", level: flowOk ? "" : "critical" },
    inputTokens: { label: "Input Token", value: compact(inputTok), note: "prompt tokens" },
    outputTokens: { label: "Output Token", value: compact(outputTok), note: "completion tokens" },
    errorRate: { label: "错误率", value: errorRateVal + "%", note: `${sum(agents, "runErrors")} of ${runs} runs` },
    diskUsage: { label: "磁盘用量", value: Number(point.diskUsedPercent || 0).toFixed(1) + "%", note: bytes(point.diskAvailableBytes) + " available" },
    memoryUsage: { label: "内存用量", value: bytes(memPoint.maxMemoryBytes || 0), note: "peak resident" },
  };
  const visibleKpis = (config?.sectionKpis?.overview || []).filter((m) => m.visible);
  const metrics = visibleKpis.map((m) => allMetrics[m.id]).filter(Boolean);
  const editBtn = sectionKpiEditor === "overview"
    ? "<button class=\"kpi-edit-btn active\" data-section-kpi-edit=\"overview\" title=\"编辑指标\">✓</button>"
    : "<button class=\"kpi-edit-btn\" data-section-kpi-edit=\"overview\" title=\"编辑指标\">✎</button>";
  const editor = sectionKpiEditor === "overview" ? sectionKpiEditorHTML("overview", config) : "";
  return panel("domain-overview-summary", "核心指标", metricStrip(metrics) + editor, "全局运行规模与健康度", "domain-full", editBtn);
}

function agentSummary(data) {
  const agents = data.agents || [];
  const sessions = data.sessions || [];
  const active = agents.filter((agent) => agentStatus(agent, sessions).state === "active").length;
  const runs = sum(agents, "runs");
  const successRate = runs ? 100 * completedRuns(agents) / runs : 100;
  return panel("domain-agent-summary", "Agent 状态", metricStrip([
    { label: "在线", value: num(active), note: `${agents.length} 个 Agent` },
    { label: "活跃 Session", value: num(sessions.filter((row) => row.status === "active").length), note: "当前运行中" },
    { label: "任务", value: compact(runs), note: `${sum(agents, "runErrors")} failed` },
    { label: "成功率", value: `${successRate.toFixed(1)}%`, note: "Agent Run" },
    { label: "Token", value: compact(sum(agents, "totalTokens")), note: "全 Agent" },
    { label: "Cost", value: money(sum(agents, "costUsd")), note: "全 Agent" },
  ]), "第一阶段", "domain-full");
}

function agentTable(data) {
  const sessions = data.sessions || [];
  const rows = (data.agents || []).map((agent) => {
    const status = agentStatus(agent, sessions);
    return `<tr>
      <td><b>${esc(agent.agentId)}</b></td>
      <td><span class="status ${status.state}">${esc(status.state)}</span></td>
      <td>${num(status.activeSessions)} / ${num(status.sessions)}</td>
      <td>${num(agent.runs)}</td><td>${Number(agent.successRate ?? (100 - Number(agent.errorRate || 0))).toFixed(1)}%</td>
      <td>${compact(agent.totalTokens)}</td><td>${money(agent.costUsd)}</td>
      <td>${money(Number(agent.costUsd || 0) / Math.max(1, Number(agent.completedRuns || 0)))}</td>
      <td>${ms(agent.averageDurationMs)}</td>
    </tr>`;
  });
  return panel("domain-agent-table", "Agent 运行明细", rows.length ? table(
    ["Agent", "状态", "活跃 / Session", "任务", "成功率", "Token", "Cost", "成功任务均价", "平均耗时"],
    rows,
  ) : empty(), "点击顶部 Agent 筛选可下钻", "domain-full");
}

function sessionsSummary(data) {
  const summary = sessionSummary(data.sessions);
  return panel("domain-session-summary", "Session 质量", metricStrip([
    { label: "Session", value: num(summary.total), note: `${summary.active} active` },
    { label: "Message", value: compact(summary.messages), note: "当前已采集口径" },
    { label: "P50", value: ms(summary.p50), note: "Session 持续时间" },
    { label: "P90", value: ms(summary.p90), note: "Session 持续时间" },
    { label: "P99", value: ms(summary.p99), note: "Session 持续时间" },
  ]), "第一阶段", "domain-full");
}

function sessionTable(data) {
  const callsBySession = new Map();
  for (const call of data.llmCalls || []) {
    if (!callsBySession.has(call.sessionId)) callsBySession.set(call.sessionId, { llm: 0, tokens: 0, cost: 0, errors: 0 });
    const item = callsBySession.get(call.sessionId);
    item.llm++;
    item.tokens += Number(call.totalTokens || 0);
    item.cost += Number(call.costUsd || 0);
    if (call.status === "failed") item.errors++;
  }
  for (const call of data.mcpCalls || []) {
    if (!callsBySession.has(call.sessionId)) callsBySession.set(call.sessionId, { llm: 0, tokens: 0, cost: 0, errors: 0 });
    const item = callsBySession.get(call.sessionId);
    if (call.status === "failed") item.errors++;
  }
  const rows = (data.sessions || []).map((session) => {
    const fallback = callsBySession.get(session.sessionId) || {};
    const calls = {
      llm: session.llmCalls ?? fallback.llm,
      tokens: session.totalTokens ?? fallback.tokens,
      cost: session.costUsd ?? fallback.cost,
      errors: session.errors ?? fallback.errors,
      tools: session.toolCalls || 0,
      retries: session.retries || 0,
    };
    return `<tr>
      <td><b>${esc(session.sessionId)}</b></td><td>${esc(session.agentId || "unknown")}</td>
      <td><span class="status ${esc(session.status)}">${esc(session.status)}</span></td>
      <td>${ms(sessionDuration(session))}</td><td>${num(session.messageCount)}</td>
      <td>${num(calls.llm)}</td><td>${num(calls.tools)}</td><td>${compact(calls.tokens)}</td><td>${money(calls.cost)}</td><td>${num(calls.errors)}</td><td>${num(calls.retries)}</td>
    </tr>`;
  });
  return panel("domain-session-list", "Session 排行与下钻", rows.length ? table(
    ["Session", "Agent", "状态", "持续时间", "Message", "LLM", "Tool", "Token", "Cost", "异常", "Retry"],
    rows,
  ) : empty(), "选择 Trace 面板中的 Session 查看完整链路", "domain-full");
}

function modelSummary(data) {
  const models = data.models || [];
  const requests = sum(models, "requests");
  const errors = sum(models, "errors");
  const outputTokens = sum(models, "outputTokens");
  const duration = models.reduce((total, row) => total + Number(row.averageDurationMs || 0) * Number(row.requests || 0), 0);
  const successes = models.reduce((total, row) => {
    const rate = row.successRate ?? (100 - Number(row.errorRate ?? (row.requests ? 100 * row.errors / row.requests : 0)));
    return total + Number(rate || 0) * Number(row.requests || 0) / 100;
  }, 0);
  return panel("domain-model-summary", "模型调用", metricStrip([
    { label: "调用次数", value: compact(requests), note: `${models.length} 个模型` },
    { label: "Token", value: compact(sum(models, "inputTokens") + outputTokens + sum(models, "cacheReadTokens") + sum(models, "cacheWriteTokens")), note: `${compact(outputTokens)} output` },
    { label: "Cache Token", value: compact(sum(models, "cacheReadTokens") + sum(models, "cacheWriteTokens")), note: "read + write" },
    { label: "Cost", value: money(sum(models, "costUsd")), note: "所选时间范围" },
    { label: "平均延迟", value: ms(requests ? duration / requests : 0), note: "端到端 LLM 调用" },
    { label: "成功率", value: `${(requests ? 100 * successes / requests : 100).toFixed(1)}%`, note: `${errors} errors` },
  ]), "第一阶段", "domain-full");
}

function modelTable(data) {
  const rows = (data.models || []).map((model) => {
    const totalTokens = Number(model.inputTokens || 0) + Number(model.outputTokens || 0) + Number(model.cacheReadTokens || 0) + Number(model.cacheWriteTokens || 0);
    return `<tr>
      <td>${esc(model.provider)} / <b>${esc(model.model)}</b></td><td>${num(model.requests)}</td>
      <td>${compact(model.inputTokens)}</td><td>${compact(model.outputTokens)}</td>
      <td>${compact(model.cacheReadTokens)}</td><td>${compact(totalTokens)}</td><td>${money(model.costUsd)}</td>
      <td>${ms(model.averageTimeToFirstTokenMs ?? model.averageTimeToFirstByteMs)}</td>
      <td>${model.generationTokensPerSecond != null ? `${Number(model.generationTokensPerSecond).toFixed(1)} tok/s` : "—"}</td>
      <td>${ms(model.averageDurationMs)}</td><td>${Number(model.errorRate ?? (model.requests ? 100 * model.errors / model.requests : 0)).toFixed(1)}%</td>
    </tr>`;
  });
  const hasTTFT = (data.models || []).some((row) => row.averageTimeToFirstTokenMs != null);
  const note = `<div class="instrumentation-note"><b>遥测口径</b><span>${hasTTFT ? "TTFT 使用 SDK 首 Token 时间戳；" : "SDK 暂未上报 TTFT 时回退展示 TTFB；"}生成速度仅在 generationDurationMs 可用时计算，不用完整调用耗时冒充。</span></div>`;
  return panel("domain-model-table", "模型统计明细", (rows.length ? table(
    ["模型", "调用", "Input", "Output", "Cache Read", "Token", "Cost", "TTFT / TTFB", "生成速度", "平均延迟", "错误率"],
    rows,
  ) : empty()) + note, "Token / Cost / 性能", "domain-full");
}

function costAttribution(data, config) {
  const trends = data.costTrends30d || [];
  const daily = new Map();
  const agents = new Map();
  for (const row of trends) {
    daily.set(row.period, (daily.get(row.period) || 0) + Number(row.costUsd || 0));
    const key = row.agentId || "unknown";
    agents.set(key, (agents.get(key) || 0) + Number(row.costUsd || 0));
  }
  const rollingCost = [...daily.values()].reduce((total, value) => total + value, 0);
  const averageDaily = rollingCost / Math.max(1, daily.size);
  const daysInMonth = new Date(new Date().getFullYear(), new Date().getMonth() + 1, 0).getDate();
  const forecast = averageDaily * daysInMonth;
  const budget = Number(config?.thresholds?.costBudgetUsd || 0);
  const budgetRatio = budget ? 100 * forecast / budget : 0;
  const sessionRows = [...(data.sessions || [])].filter((row) => Number(row.costUsd || 0) > 0)
    .sort((a, b) => Number(b.costUsd || 0) - Number(a.costUsd || 0)).slice(0, 12);
  const agentRows = [...agents.entries()].sort((a, b) => b[1] - a[1]).slice(0, 12);
  const summary = metricStrip([
    { label: "30 天成本", value: money(rollingCost), note: `${daily.size} 个采样日` },
    { label: "日均成本", value: money(averageDaily), note: "滚动 30 天" },
    { label: "月度预测", value: money(forecast), note: budget ? `预算 ${money(budget)}` : "尚未设置预算", level: budgetRatio >= 100 ? "critical" : budgetRatio >= 80 ? "warning" : "" },
    { label: "预算利用率", value: budget ? `${budgetRatio.toFixed(0)}%` : "—", note: "基于滚动日均预测" },
  ]);
  const budgetBar = budget ? `<div class="budget-meter"><span style="width:${Math.min(100, budgetRatio)}%"></span><small>${money(forecast)} forecast / ${money(budget)} budget</small></div>` : "";
  return panel("domain-cost-attribution", "成本归因与预算", summary + budgetBar + `<div class="split">
    <section><h3>Agent 成本（30 天）</h3>${agentRows.length ? table(["Agent", "Cost"], agentRows.map(([agent, cost]) => `<tr><td><b>${esc(agent)}</b></td><td>${money(cost)}</td></tr>`)) : empty()}</section>
    <section><h3>高成本 Session</h3>${sessionRows.length ? table(["Session", "Agent", "Cost", "Token"], sessionRows.map((row) => `<tr><td><b>${esc(row.sessionId)}</b></td><td>${esc(row.agentId || "unknown")}</td><td>${money(row.costUsd)}</td><td>${compact(row.totalTokens)}</td></tr>`)) : empty()}</section>
  </div>`, "Agent → Session → Model · 预算预测", "domain-full");
}

function toolSummary(data) {
  const tools = data.tools || [];
  const calls = sum(tools, "calls");
  const errors = sum(tools, "errors");
  const successes = tools.reduce((total, row) => {
    const rate = row.successRate ?? (100 - Number(row.failureRate ?? (row.calls ? 100 * row.errors / row.calls : 0)));
    return total + Number(rate || 0) * Number(row.calls || 0) / 100;
  }, 0);
  const weightedDuration = tools.reduce((total, row) => total + Number(row.averageDurationMs || 0) * Number(row.calls || 0), 0);
  return panel("domain-tool-summary", "Tool / MCP 调用", metricStrip([
    { label: "调用次数", value: compact(calls), note: `${tools.length} 个工具` },
    { label: "成功率", value: `${(calls ? 100 * successes / calls : 100).toFixed(1)}%`, note: `${errors} errors` },
    { label: "平均耗时", value: ms(calls ? weightedDuration / calls : 0), note: "加权平均" },
    { label: "P95", value: ms(percentile(tools.map((row) => row.p95DurationMs), 0.95)), note: "工具组尾延迟" },
    { label: "P99", value: ms(percentile(tools.map((row) => row.p99DurationMs), 0.99)), note: "工具组尾延迟" },
    { label: "Timeout", value: compact(sum(tools, "timeouts")), note: `${compact(sum(tools, "retries"))} retry` },
  ]), "第一阶段", "domain-full");
}

function toolTable(data) {
  const rows = (data.tools || []).map((tool) => `<tr>
    <td><span class="pill">${esc(tool.source)}</span> <b>${esc(tool.tool)}</b></td>
    <td>${num(tool.calls)}</td><td>${Number(tool.successRate ?? (100 - Number(tool.failureRate ?? (tool.calls ? 100 * tool.errors / tool.calls : 0)))).toFixed(1)}%</td>
    <td>${ms(tool.averageDurationMs)}</td><td>${ms(tool.p95DurationMs)}</td><td>${ms(tool.p99DurationMs)}</td>
    <td>${num(tool.timeouts)}</td><td>${num(tool.retries)}</td><td>${num(tool.errors)}</td>
  </tr>`);
  return panel("domain-tool-table", "调用性能明细", (rows.length ? table(
    ["Tool / MCP", "调用", "成功率", "平均耗时", "P95", "P99", "Timeout", "Retry", "Error"],
    rows,
  ) : empty()) + `<div class="instrumentation-note"><b>遥测口径</b><span>Retry 仅统计 SDK 上报的 attempt / retryReason，不从重复调用推断。</span></div>`, "调用量、失败率与尾延迟", "domain-full");
}

function toolHeatmap(data) {
  const rows = data.timeseries?.tools || [];
  if (!rows.length) return panel("domain-tool-heatmap", "Agent × Tool 热力图", empty("当前范围内没有可关联到 Agent 的 Tool / MCP 调用"), "调用次数", "domain-full");
  const agents = [...new Set(rows.map((row) => row.agentId || "unknown"))].slice(0, 12);
  const tools = [...new Set(rows.map((row) => row.tool || "unknown"))].slice(0, 18);
  const lookup = new Map();
  for (const row of rows) {
    const key = `${row.agentId || "unknown"}|${row.tool || "unknown"}`;
    lookup.set(key, (lookup.get(key) || 0) + Number(row.calls || 0));
  }
  const max = Math.max(1, ...lookup.values());
  const body = `<div class="matrix-wrap"><div class="matrix" style="--matrix-columns:${tools.length}">
    <div></div>${tools.map((tool) => `<b title="${esc(tool)}">${esc(tool)}</b>`).join("")}
    ${agents.map((agent) => `<strong>${esc(agent)}</strong>${tools.map((tool) => {
      const value = lookup.get(`${agent}|${tool}`) || 0;
      return `<i style="--heat:${value / max}" title="${esc(agent)} × ${esc(tool)}: ${value}">${value || ""}</i>`;
    }).join("")}`).join("")}
  </div></div>`;
  return panel("domain-tool-heatmap", "Agent × Tool 热力图", body, "调用次数", "domain-full");
}

function infrastructureSummary(data) {
  const point = [...(data.timeseries?.points || [])].reverse().find((row) => Number(row.diskTotalBytes || row.averageMemoryBytes || 0) > 0) || {};
  const body = metricStrip([
    { label: "CPU", value: `${Number(point.averageCpuPercent || 0).toFixed(1)}%`, note: "Observatory 采样" },
    { label: "Memory", value: `${(Number(point.averageMemoryBytes || 0) / 1048576).toFixed(1)} MiB`, note: "Resident memory" },
    { label: "Disk", value: `${Number(point.diskUsedPercent || 0).toFixed(1)}%`, note: "宿主机磁盘" },
    { label: "Daemon", value: data.status?.daemon?.ready ? "Ready" : "Not ready", note: data.status?.daemon?.version || "unknown" },
  ]) + `<div class="integration-callout"><div><b>Prometheus + Grafana</b><span>Network、Load、进程明细与长期资源留存由基础设施监控承接；Observatory 只保留定位 AI 调用链所需的资源上下文。</span></div><span class="integration-badge">推荐集成</span></div>`;
  return panel("domain-infrastructure-summary", "基础设施摘要", body, "职责边界", "domain-full");
}

function errorsSummary(data, alerts = []) {
  const errors = data.errors || [];
  const byKind = (kind) => errors.filter((row) => row.kind === kind).reduce((total, row) => total + Number(row.errors || 0), 0);
  return panel("domain-errors-summary", "异常总览", metricStrip([
    { label: "Agent Error", value: compact(byKind("run")), note: "Run failed" },
    { label: "LLM Error", value: compact(byKind("llm")), note: "模型调用" },
    { label: "Tool Error", value: compact(byKind("tool") + byKind("mcp")), note: "Tool + MCP" },
    { label: "System Error", value: compact(byKind("system")), note: "Gateway / monitor" },
    { label: "异常类别", value: num(errors.length), note: "聚合后" },
    { label: "活动告警", value: num(alerts.filter((item) => !item.acknowledged && !item.silenced).length), note: `${alerts.filter((item) => item.severity === "critical").length} critical` },
  ]), "诊断入口", "domain-full");
}

function alertCenter(alerts = []) {
  const visible = alerts.filter((item) => !item.silenced);
  const body = visible.length ? `<div class="alert-list">${visible.map((item) => `<article class="alert-item ${esc(item.severity)} ${item.acknowledged ? "acknowledged" : ""}">
    <i></i><div><b>${esc(item.title)}</b><span>${esc(item.detail)}</span><small>${esc(item.source)} · ${esc(item.target)}${item.occurredAt ? ` · ${esc(shortTime(item.occurredAt))}` : ""}</small></div>
    <div>${item.acknowledged ? `<span class="alert-ack">已确认</span>` : `<button data-alert-ack="${esc(item.id)}">确认</button>`}<button data-alert-silence="${esc(item.id)}">静默 1h</button></div>
  </article>`).join("")}</div>` : empty("当前没有触发中的告警");
  return panel("domain-alert-center", "活动告警", body + `<div class="alert-footer"><span>同一规则与对象使用稳定 ID 去重；信号恢复后自动离开活动列表。</span><button id="alert-state-reset">清除确认/静默</button></div>`, `${visible.length} active`, "domain-full");
}

function errorsTable(data) {
  const rows = (data.errors || []).map((error) => `<tr>
    <td><span class="pill">${esc(error.kind)}</span></td><td><b>${esc(error.category)}</b></td>
    <td>${num(error.errors)}</td><td>${ms(error.averageDurationMs)}</td><td>${esc(shortTime(error.lastOccurredAt))}</td>
  </tr>`);
  const recentRows = (data.anomalies || []).slice(0, 20).map((event) => `<tr><td><span class="pill">${esc(event.kind)}</span></td><td><b>${esc(event.label || "unknown")}</b></td><td>${esc(event.agentId || "unknown")}</td><td>${esc(event.category || "unknown")}</td><td>${esc(shortTime(event.occurredAt))}</td></tr>`);
  return panel("domain-errors-table", "错误聚合与最近事件", `<div class="split">
    <section><h3>错误类型</h3>${rows.length ? table(["来源", "类别", "次数", "平均耗时", "最近发生"], rows) : empty("当前范围内没有错误")}</section>
    <section><h3>最近异常事件</h3>${recentRows.length ? table(["来源", "对象", "Agent", "错误", "时间"], recentRows) : empty("当前范围内没有异常事件")}</section>
  </div>`, "Agent / LLM / Tool / System", "domain-full");
}

export function domainSummaryHTML(domain, data, config, sessionDetail, kpiEditorOpen, alerts = [], sectionKpiEditor = null) {
  if (domain === "overview") return overviewSummary(data, config, sectionKpiEditor);
  if (domain === "agents") return agentSummary(data);
  if (domain === "sessions") return sessionsSummary(data);
  if (domain === "models") return modelSummary(data);
  if (domain === "tools") return toolSummary(data);
  if (domain === "infrastructure") return infrastructureSummary(data);
  if (domain === "errors") return errorsSummary(data, alerts);
  return moduleHTML("overview", data, config, sessionDetail, kpiEditorOpen, { draggable: false, sectionKpiEditor });
}

export function domainDetailHTML(domain, data, config, sessionDetail, kpiEditorOpen, alerts = [], sectionKpiEditor = null) {
  if (domain === "overview") {
    return moduleHTML("cost_trends", data, config, sessionDetail, kpiEditorOpen, { draggable: false, sectionKpiEditor })
      + moduleHTML("errors_cost", data, config, sessionDetail, kpiEditorOpen, { draggable: false, sectionKpiEditor });
  }
  if (domain === "agents") {
    return moduleHTML("heatmap", data, config, sessionDetail, kpiEditorOpen, { draggable: false, sectionKpiEditor }) + agentTable(data);
  }
  if (domain === "sessions") {
    return moduleHTML("sessions", data, config, sessionDetail, kpiEditorOpen, { draggable: false, sectionKpiEditor }) + sessionTable(data);
  }
  if (domain === "models") return modelTable(data) + costAttribution(data, config);
  if (domain === "tools") return toolHeatmap(data) + toolTable(data);
  if (domain === "errors") return alertCenter(alerts) + errorsTable(data);
  return "";
}
