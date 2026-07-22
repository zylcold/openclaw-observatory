const STATE_KEY = "openclaw-observatory-alert-state-v1";

function rate(errors, total) {
  return total > 0 ? 100 * Number(errors || 0) / total : 0;
}

function alert(id, severity, title, detail, source, target, occurredAt = "") {
  return { id, severity, title, detail, source, target, occurredAt };
}

export function evaluateAlerts(data, config, now = Date.now()) {
  if (!data) return [];
  const t = config?.thresholds || {};
  const warning = Number(t.errorRateWarning ?? 5);
  const critical = Number(t.errorRateCritical ?? 15);
  const alerts = [];

  for (const row of data.agents || []) {
    const total = Number(row.runs || 0);
    const value = rate(row.runErrors, total);
    if (total >= 3 && value >= warning) alerts.push(alert(
      `agent-error:${row.agentId}`, value >= critical ? "critical" : "warning",
      `Agent ${row.agentId} 失败率升高`, `${value.toFixed(1)}% · ${row.runErrors}/${total} runs`,
      "agent", row.agentId,
    ));
  }
  for (const row of data.models || []) {
    const total = Number(row.requests || 0);
    const value = Number(row.errorRate ?? rate(row.errors, total));
    if (total >= 3 && value >= warning) alerts.push(alert(
      `model-error:${row.provider}:${row.model}`, value >= critical ? "critical" : "warning",
      `${row.model} 错误率升高`, `${value.toFixed(1)}% · ${row.errors}/${total} calls`,
      "model", row.model,
    ));
    const latency = Number(row.averageDurationMs || 0);
    if (total >= 3 && latency >= Number(t.llmLatencyWarningMs ?? 5000)) alerts.push(alert(
      `model-latency:${row.provider}:${row.model}`, latency >= Number(t.llmLatencyCriticalMs ?? 15000) ? "critical" : "warning",
      `${row.model} 延迟升高`, `平均 ${(latency / 1000).toFixed(1)}s · ${total} calls`,
      "model", row.model,
    ));
  }
  for (const row of data.tools || []) {
    const total = Number(row.calls || 0);
    const value = Number(row.failureRate ?? rate(row.errors, total));
    if (total >= 3 && value >= warning) alerts.push(alert(
      `tool-error:${row.source}:${row.tool}`, value >= critical ? "critical" : "warning",
      `${row.tool} 失败率升高`, `${value.toFixed(1)}% · ${row.errors}/${total} calls`,
      row.source || "tool", row.tool,
    ));
    const p99 = Number(row.p99DurationMs || 0);
    if (total >= 3 && p99 >= Number(t.toolP99WarningMs ?? 5000)) alerts.push(alert(
      `tool-latency:${row.source}:${row.tool}`, p99 >= Number(t.toolP99CriticalMs ?? 15000) ? "critical" : "warning",
      `${row.tool} P99 延迟升高`, `P99 ${(p99 / 1000).toFixed(1)}s · ${total} calls`,
      row.source || "tool", row.tool,
    ));
  }
  const stuckMs = Number(t.sessionStuckMs ?? 3600000);
  for (const row of data.sessions || []) {
    const age = now - Date.parse(row.startedAt);
    if (row.status === "active" && Number.isFinite(age) && age >= stuckMs) alerts.push(alert(
      `session-stuck:${row.sessionId}`, age >= stuckMs * 2 ? "critical" : "warning",
      `Session 可能卡住`, `${row.agentId || "unknown"} · 已运行 ${(age / 3600000).toFixed(1)}h`,
      "session", row.sessionId, row.startedAt,
    ));
  }
  for (const row of (data.anomalies || []).filter((item) => item.kind === "system")) {
    alerts.push(alert(
      `system:${row.id}`, "critical", row.label || "系统异常", row.category || "system event",
      "system", row.id, row.occurredAt,
    ));
  }
  const budget = Number(t.costBudgetUsd || 0);
  const monthlyCost = Number(data.costSummary?.lastMonthCost || 0);
  if (budget > 0 && monthlyCost >= budget * 0.8) alerts.push(alert(
    "cost-budget:monthly", monthlyCost >= budget ? "critical" : "warning",
    monthlyCost >= budget ? "月度预算已超限" : "月度预算接近上限",
    `$${monthlyCost.toFixed(2)} / $${budget.toFixed(2)}`, "cost", "monthly",
  ));

  // --- Daemon health & data-flow alerts ---
  const status = data.status || {};
  const daemon = status.daemon || {};

  // Daemon not ready
  if (daemon.ready === false) alerts.push(alert(
    "daemon:not-ready", "critical",
    "Observatory daemon 未就绪",
    `daemon version ${daemon.version || "unknown"} — 数据采集可能中断`, "system", "observatoryd",
  ));

  // Last event stale (>10min no events at all)
  const lastEvent = status.lastEventReceivedAt ? Date.parse(status.lastEventReceivedAt) : 0;
  if (lastEvent && Number.isFinite(lastEvent)) {
    const eventAgeMin = (now - lastEvent) / 60000;
    if (eventAgeMin >= 10) alerts.push(alert(
      "daemon:event-stale", eventAgeMin >= 30 ? "critical" : "warning",
      "Observatory 事件流中断",
      `最后一次事件在 ${Math.round(eventAgeMin)} 分钟前 — daemon 可能卡死或 socket 断开`, "system", "observatoryd",
      new Date(lastEvent).toISOString(),
    ));
  }

  // Data flow stall: resource samples flowing but no agent activity
  const points = data.timeseries?.points || [];
  if (points.length >= 5) {
    const recent = points.slice(-5);
    const hasResource = recent.some((p) => Number(p.averageCpuPercent || 0) > 0);
    const hasActivity = recent.some((p) => Number(p.runs || 0) > 0 || Number(p.llmRequests || 0) > 0);
    if (hasResource && !hasActivity) {
      const allZero = recent.every((p) => Number(p.runs || 0) === 0 && Number(p.llmRequests || 0) === 0 && Number(p.toolCalls || 0) === 0);
      if (allZero) alerts.push(alert(
        "daemon:data-flow-stall", "critical",
        "Agent 活动数据缺失",
        "CPU/内存采样正常但 runs/LLM/tool 全为零 — Gateway 监控插件可能断连", "system", "observatoryd",
      ));
    }
  }

  // Instance offline
  for (const inst of status.instances || []) {
    if (inst.status === "down") alerts.push(alert(
      `instance:down:${inst.instanceId}`, "critical",
      `实例 ${inst.instanceId?.slice(0, 16)} 离线`, "OpenClaw 实例不可达", "system", inst.instanceId,
    ));
    const lastSeen = inst.lastSeenAt ? Date.parse(inst.lastSeenAt) : 0;
    if (lastSeen && Number.isFinite(lastSeen)) {
      const ageMin = (now - lastSeen) / 60000;
      if (ageMin >= 5 && inst.status !== "down") alerts.push(alert(
        `instance:stale:${inst.instanceId}`, ageMin >= 15 ? "critical" : "warning",
        `实例 ${inst.instanceId?.slice(0, 16)} 心跳延迟`, `最后心跳 ${Math.round(ageMin)} 分钟前`, "system", inst.instanceId,
      ));
    }
  }

  return alerts.sort((a, b) => (a.severity === b.severity ? 0 : a.severity === "critical" ? -1 : 1));
}

export function loadAlertState(storage = globalThis.localStorage) {
  try {
    const value = JSON.parse(storage?.getItem(STATE_KEY) || "{}");
    return { acknowledged: value.acknowledged || {}, silenced: value.silenced || {} };
  } catch {
    return { acknowledged: {}, silenced: {} };
  }
}

export function saveAlertState(state, storage = globalThis.localStorage) {
  storage?.setItem(STATE_KEY, JSON.stringify(state));
  return state;
}

export function applyAlertState(alerts, state, now = Date.now()) {
  return (alerts || []).map((item) => ({
    ...item,
    acknowledged: Boolean(state?.acknowledged?.[item.id]),
    silencedUntil: Number(state?.silenced?.[item.id] || 0),
    silenced: Number(state?.silenced?.[item.id] || 0) > now,
  }));
}
