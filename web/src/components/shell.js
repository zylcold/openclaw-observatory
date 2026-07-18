import { esc } from "../format.js";
import { RANGE_KEYS } from "../state.js";
import { getPricingTimestamp } from "../pricing.js";
import { customChartBuilderHTML, customChartPanelHTML } from "./custom-charts.js";
import { OBSERVABILITY_DOMAINS, chartsForDomain, observabilityDomain } from "../observability-model.js";
import { domainDetailHTML, domainSummaryHTML } from "./domain-views.js";

function fmtLocal(iso) {
  const d = new Date(iso);
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

export function shell({
  config, data, filters, filterOptions, viewFilters, activeDomain, loading, error,
  settingsOpen, sessionDetail, connectionLost, dataStale, kpiEditorOpen, customBuilder, alerts = [],
}) {
  const instances = data?.status?.instances || [];
  const agents = data?.agents || [];
  const gatewayUp = instances.some((x) => x.status === "up");
  const compatible = !data || (data.status?.apiVersion === 3 && data.status?.capabilities?.includes("timeseries-v3"));
  const domain = observabilityDomain(activeDomain);
  const charts = chartsForDomain(config.customCharts, domain.id);
  const modelOptions = [...new Set([...(filterOptions?.models || []), viewFilters.model].filter(Boolean))].sort();
  const toolOptions = [...new Set([...(filterOptions?.tools || []), viewFilters.tool].filter(Boolean))].sort();
  const contextualFilters = `
    <div class="context-filters" aria-label="观测维度筛选">
      <span>维度筛选</span>
      <select id="model-filter" aria-label="Model"><option value="">全部 Model</option>${modelOptions.map((model) => `<option value="${esc(model)}" ${viewFilters.model === model ? "selected" : ""}>${esc(model)}</option>`).join("")}</select>
      <select id="tool-filter" aria-label="Tool"><option value="">全部 Tool / MCP</option>${toolOptions.map((tool) => `<option value="${esc(tool)}" ${viewFilters.tool === tool ? "selected" : ""}>${esc(tool)}</option>`).join("")}</select>
      <select id="status-filter" aria-label="状态"><option value="">全部状态</option>${(filterOptions?.statuses || []).map((status) => `<option value="${status}" ${viewFilters.status === status ? "selected" : ""}>${status}</option>`).join("")}</select>
      ${viewFilters.model || viewFilters.tool || viewFilters.status ? `<button id="context-filter-reset">清除</button>` : ""}
    </div>`;
  return `
    <nav class="topbar">
      <div class="brand"><i></i><div><strong>OpenClaw Observatory</strong><span>v0.4</span></div></div>
      <div class="toolbar">
        <div class="gateway"><i class="${gatewayUp ? "up" : "down"}"></i>${gatewayUp ? "Gateway online" : "Gateway offline"}</div>
        <div class="ranges">${RANGE_KEYS.map((key) => `<button data-range="${key}" class="${filters.range === key ? "active" : ""}">${key}</button>`).join("")}</div>
        <select id="instance-filter" aria-label="实例"><option value="">全部实例</option>${instances.map((x) => `<option value="${esc(x.instanceId)}" ${filters.instanceId === x.instanceId ? "selected" : ""}>${esc(x.instanceId)}</option>`).join("")}</select>
        <select id="agent-filter" aria-label="Agent"><option value="">全部 Agent</option>${agents.map((x) => `<option value="${esc(x.agentId)}" ${filters.agentId === x.agentId ? "selected" : ""}>${esc(x.agentId)}</option>`).join("")}</select>
        <button id="theme-toggle" class="icon-button" title="切换主题">${config.theme === "dark" ? "☀︎" : "☾"}</button>
        <button id="settings-toggle" class="icon-button" title="面板配置">⚙</button>
      </div>
    </nav>
    <nav class="domain-nav" aria-label="观测域">
      <div>${OBSERVABILITY_DOMAINS.map((item) => `<button data-observability-domain="${item.id}" class="${domain.id === item.id ? "active" : ""}">
        <i>${esc(item.glyph)}</i><span><b>${esc(item.label)}</b><small>${esc(item.name)}</small></span><em>${item.id === "errors" && alerts.filter((alert) => !alert.acknowledged && !alert.silenced).length ? alerts.filter((alert) => !alert.acknowledged && !alert.silenced).length : esc(item.phase)}</em>
      </button>`).join("")}</div>
    </nav>
    ${!compatible ? `<div class="banner">前后端版本不匹配：面板需要 API v3 / timeseries-v3。</div>` : ""}
    ${connectionLost ? `<div class="banner">正在重连…${dataStale ? " 当前显示的数据可能已过期。" : ""}</div>` : ""}
    <main>
      <div class="page-title"><div><span class="page-kicker">${esc(domain.label)} · ${esc(domain.phase)}</span><h1>${esc(domain.name)}</h1><p>${esc(domain.description)} · ${filters.range.toUpperCase()}　${filters.instanceId ? '实例 ' + esc(filters.instanceId) + ' · ' : ''}${filters.agentId ? 'Agent ' + esc(filters.agentId) + ' · ' : ''}${fmtLocal(filters.from)} — ${fmtLocal(filters.to)}</p></div><div class="page-actions"><button id="custom-chart-create" class="create-chart-button"><span>＋</span>创建图表</button><button id="refresh">${loading ? "刷新中…" : "立即刷新"}</button></div></div>
      ${contextualFilters}
      ${error ? `<div class="banner error">${esc(error)}</div>` : ""}
      <section id="dashboard" class="dashboard domain-${domain.id} ${loading && !data ? "loading" : ""}">${data
        ? domainSummaryHTML(domain.id, data, config, sessionDetail, kpiEditorOpen, alerts)
          + charts.map((item) => customChartPanelHTML(item, data)).join("")
          + domainDetailHTML(domain.id, data, config, sessionDetail, kpiEditorOpen, alerts)
        : `<div class="skeleton">正在加载观测指标…</div>`}</section>
    </main>
    <div class="drawer-backdrop ${settingsOpen ? "open" : ""}" id="drawer-backdrop"></div>
    <aside class="drawer ${settingsOpen ? "open" : ""}">
      <header><div><h2>Dashboard 配置</h2><p>7 个观测域、自定义图表、刷新与阈值均保存为 JSON</p></div><button id="settings-close">×</button></header>
      <label>自动刷新<select id="refresh-interval"><option value="5000">5 秒</option><option value="15000">15 秒</option><option value="30000">30 秒</option><option value="60000">60 秒</option><option value="0">关闭</option></select></label>
      <div class="drawer-domain-note"><b>观测域结构</b><span>Overview 保留核心指标；其他图表在对应观测域通过“创建图表”添加、删除和排序。</span></div>
      <div class="alert-settings">
        <div><label>月度成本预算（USD）<input id="cost-budget" type="number" min="0" step="1" value="${Number(config.thresholds.costBudgetUsd || 0)}"></label>
        <label>Session 卡住阈值（小时）<input id="session-stuck-hours" type="number" min="0.1" step="0.5" value="${Number(config.thresholds.sessionStuckMs || 3600000) / 3600000}"></label></div>
        <small>错误率、模型延迟与 Tool P99 阈值可在下方 JSON 中精细调整。</small>
      </div>
      <div class="pricing-section"><div class="pricing-header"><span>模型定价</span><small id="pricing-timestamp">${(() => { const ts = getPricingTimestamp(); return ts ? `更新于 ${ts.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}` : "未获取"; })()}</small></div><button id="refresh-pricing" class="drawer-button">从 OpenRouter 刷新定价</button><small class="pricing-hint">成本基于 token 用量 × 模型定价计算</small></div>
      <label>Dashboard JSON<textarea id="config-json" spellcheck="false">${esc(JSON.stringify(config, null, 2))}</textarea></label>
      <div class="drawer-actions"><button id="config-reset">恢复默认</button><button id="config-save" class="primary">应用 JSON</button></div>
    </aside>
    ${customChartBuilderHTML(customBuilder, domain.id)}`;
}
