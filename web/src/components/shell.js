import { MODULES } from "../config.js";
import { esc } from "../format.js";
import { RANGE_KEYS } from "../state.js";
import { getPricingTimestamp } from "../pricing.js";
import { moduleHTML } from "./modules.js";

function fmtLocal(iso) {
  const d = new Date(iso);
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

export function shell({ config, data, filters, loading, error, settingsOpen, sessionDetail, connectionLost, dataStale, kpiEditorOpen }) {
  const instances = data?.status?.instances || [];
  const agents = data?.agents || [];
  const gatewayUp = instances.some((x) => x.status === "up");
  const compatible = !data || (data.status?.apiVersion === 3 && data.status?.capabilities?.includes("timeseries-v3"));
  const visible = config.modules.filter((m) => m.visible);
  const moduleNames = new Map(MODULES);
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
    ${!compatible ? `<div class="banner">前后端版本不匹配：面板需要 API v3 / timeseries-v3。</div>` : ""}
    ${connectionLost ? `<div class="banner">正在重连…${dataStale ? " 当前显示的数据可能已过期。" : ""}</div>` : ""}
    <main>
      <div class="page-title"><div><h1>运行概览</h1><p>${filters.range.toUpperCase()}　${filters.instanceId ? '实例 ' + esc(filters.instanceId) + ' · ' : ''}${filters.agentId ? 'Agent ' + esc(filters.agentId) + ' · ' : ''}${fmtLocal(filters.from)} — ${fmtLocal(filters.to)}</p></div><button id="refresh">${loading ? "刷新中…" : "立即刷新"}</button></div>
      ${error ? `<div class="banner error">${esc(error)}</div>` : ""}
      <section id="dashboard" class="dashboard ${loading && !data ? "loading" : ""}">${data ? visible.map((m) => moduleHTML(m.id, data, config, sessionDetail, kpiEditorOpen)).join("") : `<div class="skeleton">正在加载 v0.4 指标…</div>`}</section>
    </main>
    <div class="drawer-backdrop ${settingsOpen ? "open" : ""}" id="drawer-backdrop"></div>
    <aside class="drawer ${settingsOpen ? "open" : ""}">
      <header><div><h2>Dashboard 配置</h2><p>显隐、顺序、刷新与阈值均保存为 JSON</p></div><button id="settings-close">×</button></header>
      <label>自动刷新<select id="refresh-interval"><option value="5000">5 秒</option><option value="15000">15 秒</option><option value="30000">30 秒</option><option value="60000">60 秒</option><option value="0">关闭</option></select></label>
      <fieldset><legend>模块显隐</legend>${config.modules.map((m) => `<label class="check"><input type="checkbox" data-module-visible="${m.id}" ${m.visible ? "checked" : ""}>${esc(moduleNames.get(m.id))}</label>`).join("")}</fieldset>
      <div class="pricing-section"><div class="pricing-header"><span>模型定价</span><small id="pricing-timestamp">${(() => { const ts = getPricingTimestamp(); return ts ? `更新于 ${ts.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}` : "未获取"; })()}</small></div><button id="refresh-pricing" class="drawer-button">从 OpenRouter 刷新定价</button><small class="pricing-hint">成本基于 token 用量 × 模型定价计算</small></div>
      <label>Dashboard JSON<textarea id="config-json" spellcheck="false">${esc(JSON.stringify(config, null, 2))}</textarea></label>
      <div class="drawer-actions"><button id="config-reset">恢复默认</button><button id="config-save" class="primary">应用 JSON</button></div>
    </aside>`;
}
