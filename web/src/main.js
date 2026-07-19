import "./styles.css";
import { loadDashboard, loadSession } from "./api.js";
import { loadConfig, resetConfig, saveConfig } from "./config.js";
import { destroyCharts, setChartAnimation } from "./charts.js";
import {
  chartTypeDimensionLimit, chartTypeMetricSlots, chartTypeMetrics,
  defaultCustomChartTitle, dimensionGroupById,
} from "./custom-chart-model.js";
import {
  chartsForView, domainFilterOptions, filterDashboardData, normalizeDomain,
} from "./observability-model.js";
import { timeFilters } from "./state.js";
import { fetchModelPricing } from "./pricing.js";
import { paintCharts, updateCharts, updateNonChartDOM, updateAgentTable, setRange } from "./components/modules.js";
import { shell } from "./components/shell.js";
import { applyAlertState, evaluateAlerts, loadAlertState, saveAlertState } from "./alert-model.js";

const app = document.getElementById("app");
let config = loadConfig();
let filters = timeFilters("24h");
let activeDomain = "overview";
let viewFilters = { model: "", tool: "", status: "" };
let data = null;
let sessionDetail = null;
let loading = false;
let error = "";
let settingsOpen = false;
let kpiEditorOpen = false;
let sectionKpiEditor = null; // section id whose KPI editor is open
let customBuilder = { open: false, step: 1, chartType: "", dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "", width: "half" };
let refreshTimer = null;
let streamTimer = null;
let interactionTimer = null;
let interactionUntil = 0;
let pointerActive = false;
let dragActive = false;
let openSelect = null;
let deferredRender = false;
let hasRenderedData = false;
let lastRenderKey = ""; // track filter changes to force full re-render
let connectionLost = navigator.onLine === false;
let alertState = loadAlertState();
let dataStale = false;
let refreshFailures = 0;
const domainScrollCache = new Map();
let streamBackoffMs = 1000;
let streamReconnectTimer = null;

const INTERACTION_IDLE_MS = 600;
const FILTER_KEY = "openclaw-observatory-filters-v1";

function interactionActive() {
  return settingsOpen || customBuilder.open || pointerActive || dragActive || Boolean(openSelect?.isConnected) || Date.now() < interactionUntil;
}

function flushDeferredRender() {
  clearTimeout(interactionTimer);
  if (interactionActive()) {
    interactionTimer = setTimeout(flushDeferredRender, Math.max(250, interactionUntil - Date.now()));
    return;
  }
  if (deferredRender) {
    deferredRender = false;
    render({ preserveView: true });
  }
}

function markInteraction(delay = INTERACTION_IDLE_MS) {
  interactionUntil = Math.max(interactionUntil, Date.now() + delay);
  clearTimeout(interactionTimer);
  interactionTimer = setTimeout(flushDeferredRender, delay);
}

function closeSelect(select) {
  if (openSelect !== select) return;
  openSelect = null;
  markInteraction(100);
}

function bindInteractionGuard() {
  document.addEventListener("pointerdown", (event) => {
    pointerActive = true;
    const select = event.target.closest?.("select");
    if (select) openSelect = select;
    else if (openSelect && openSelect !== event.target) openSelect = null;
    markInteraction();
  }, { capture: true, passive: true });
  document.addEventListener("pointerup", () => { pointerActive = false; markInteraction(); }, { capture: true, passive: true });
  document.addEventListener("pointercancel", () => { pointerActive = false; markInteraction(); }, { capture: true, passive: true });
  window.addEventListener("blur", () => { pointerActive = false; markInteraction(); }, { passive: true });
  document.addEventListener("scroll", () => markInteraction(), { capture: true, passive: true });
  document.addEventListener("change", (event) => closeSelect(event.target), true);
  document.addEventListener("focusout", (event) => closeSelect(event.target), true);
  document.addEventListener("keydown", (event) => {
    if ((event.key === "Escape" || event.key === "Enter") && openSelect) closeSelect(openSelect);
  }, true);
}

function captureView() {
  const elements = [];
  document.querySelectorAll("[data-module]").forEach((panel) => {
    panel.querySelectorAll(".table-wrap,.waterfall,.heatmap").forEach((element, index) => {
      elements.push({ key: `${panel.dataset.module}:${index}`, top: element.scrollTop, left: element.scrollLeft });
    });
  });
  return { windowX: window.scrollX, windowY: window.scrollY, elements };
}

function restoreView(view) {
  if (!view) return;
  for (const item of view.elements) {
    const [moduleId, index] = item.key.split(":");
    const elements = document.querySelectorAll(`[data-module="${moduleId}"] .table-wrap,[data-module="${moduleId}"] .waterfall,[data-module="${moduleId}"] .heatmap`);
    const element = elements[Number(index)];
    if (element) { element.scrollTop = item.top; element.scrollLeft = item.left; }
  }
  window.scrollTo(view.windowX, view.windowY);
}

function scrollDomainNavToActive() {
  const nav = document.querySelector('.domain-nav > div');
  const active = document.querySelector('.domain-nav button.active');
  if (!nav || !active) return;
  const navRect = nav.getBoundingClientRect();
  const activeRect = active.getBoundingClientRect();
  if (activeRect.left < navRect.left || activeRect.right > navRect.right) {
    active.scrollIntoView({ inline: 'center', block: 'nearest' });
  }
}

function render({ preserveView = false, deferWhileInteracting = false } = {}) {
  if (deferWhileInteracting && interactionActive()) {
    deferredRender = true;
    markInteraction();
    return;
  }
  try {
    deferredRender = false;
    const view = preserveView ? captureView() : null;
    destroyCharts();
    setChartAnimation(!preserveView && !hasRenderedData);
    setRange(filters.range);
    document.documentElement.dataset.theme = config.theme;
    const viewData = filterDashboardData(data, viewFilters);
    const alerts = applyAlertState(evaluateAlerts(viewData, config), alertState);
    const viewConfig = { ...config, customCharts: chartsForView(config.customCharts, activeDomain) };
    app.innerHTML = shell({
      config, data: viewData, filters, filterOptions: domainFilterOptions(data), viewFilters, activeDomain,
      loading, error, settingsOpen, sessionDetail, connectionLost, dataStale, kpiEditorOpen, customBuilder, alerts, sectionKpiEditor,
    });
    const interval = document.getElementById("refresh-interval");
    if (interval) interval.value = String(config.refreshInterval);
    bind();
    if (viewData) requestAnimationFrame(() => {
      try { paintCharts(viewData, viewConfig); } catch(e) { console.error("paintCharts error:", e); }
      restoreView(view);
      scrollDomainNavToActive();
      hasRenderedData = true;
    });
    if (!viewData) requestAnimationFrame(() => scrollDomainNavToActive());
  } catch(e) {
    console.error("render error:", e);
    app.innerHTML = '<div style="padding:40px;color:#ff647c;font-family:monospace;white-space:pre-wrap">Render Error: ' + (e.stack || e.message || String(e)) + '</div>';
  }
}

/**
 * Incrementally update charts and KPI text without full re-render.
 * Falls back to full render if charts don't exist yet.
 */
function incrementalUpdate() {
  if (!data || !hasRenderedData) return false;
  const app = document.getElementById("app");
  if (!app) return false;
  // Domain summaries and detail tables are data-derived HTML. Re-render them
  // together so chart and tabular values always represent the same snapshot.
  if (app.querySelector(".domain-panel")) return false;
  const viewData = filterDashboardData(data, viewFilters);
  const viewConfig = { ...config, customCharts: chartsForView(config.customCharts, activeDomain) };
  const ok = updateCharts(viewData, viewConfig);
  if (!ok) return false;
  updateNonChartDOM(app, viewData, viewConfig);
  updateAgentTable(app, viewData);
  return true;
}

async function refresh({ keepRange = false, automatic = false, forceRender = false } = {}) {
  if (loading) return;
  if (automatic && navigator.onLine === false) return;
  if (automatic && interactionActive()) {
    schedule(750);
    return;
  }
  const background = Boolean(data);
  const filterKey = `${filters.range}|${filters.instanceId}|${filters.agentId}|${viewFilters.status}`;
  const filtersChanged = forceRender || filterKey !== lastRenderKey;
  loading = true; error = "";
  if (!keepRange) filters = timeFilters(filters.range, filters.instanceId, filters.agentId);
  if (!background) render();
  else {
    document.documentElement.classList.add("refreshing");
    const button = document.getElementById("refresh");
    if (button) button.textContent = "刷新中…";
  }
  try {
    data = await loadDashboard({ ...filters, status: viewFilters.status });
    connectionLost = false;
    dataStale = false;
    refreshFailures = 0;
    const exists = data.sessions.some((s) => s.sessionId === sessionDetail?.sessionId);
    if (!exists) sessionDetail = data.sessions[0] ? await loadSession(data.sessions[0].sessionId) : null;
  } catch (reason) {
    error = reason instanceof Error ? reason.message : String(reason);
    connectionLost = true;
    dataStale = Boolean(data);
    refreshFailures++;
  } finally {
    loading = false;
    document.documentElement.classList.remove("refreshing");
    const btn = document.getElementById("refresh");
    if (btn) btn.textContent = "刷新";
    // For background refreshes with no filter changes, try incremental update first.
    // When filters changed (range/instance/agent), force full re-render so all panels update.
    if (background && !error && hasRenderedData && !filtersChanged) {
      if (!incrementalUpdate()) {
        render({ preserveView: true, deferWhileInteracting: true });
      }
    } else {
      lastRenderKey = filterKey;
      render({ preserveView: background && !filtersChanged, deferWhileInteracting: background });
    }
    const delay = refreshFailures >= 3
      ? Math.min(60_000, Math.max(config.refreshInterval, 1000) * 2 ** Math.min(refreshFailures - 3, 5))
      : config.refreshInterval;
    schedule(delay);
  }
}

function schedule(delay = config.refreshInterval) {
  clearTimeout(refreshTimer);
  if (config.refreshInterval > 0 && navigator.onLine !== false) refreshTimer = setTimeout(() => refresh({ automatic: true }), delay);
}

function updateConfig(next) {
  config = saveConfig(next);
  render();
  schedule();
}

function bind() {
  document.querySelectorAll("[data-observability-domain]").forEach((button) => button.addEventListener("click", () => {
    const newDomain = normalizeDomain(button.dataset.observabilityDomain);
    const currentScroll = { x: window.scrollX, y: window.scrollY };
    domainScrollCache.set(activeDomain, currentScroll);
    const savedScroll = domainScrollCache.get(newDomain) || currentScroll;
    activeDomain = newDomain;
    customBuilder = { open: false, step: 1, chartType: "", dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "", width: "half" };
    writeURLState();
    render({ preserveView: false });
    requestAnimationFrame(() => {
      window.scrollTo(savedScroll.x, savedScroll.y);
      scrollDomainNavToActive();
    });
  }));
  document.querySelectorAll("[data-range]").forEach((button) => button.addEventListener("click", () => {
    filters = timeFilters(button.dataset.range, filters.instanceId, filters.agentId); writeURLState(); refresh({ keepRange: true, forceRender: true });
  }));
  document.getElementById("instance-filter")?.addEventListener("change", (event) => { filters.instanceId = event.target.value; writeURLState(); refresh({ forceRender: true }); });
  document.getElementById("agent-filter")?.addEventListener("change", (event) => { filters.agentId = event.target.value; writeURLState(); refresh({ forceRender: true }); });
  document.getElementById("model-filter")?.addEventListener("change", (event) => {
    viewFilters.model = event.target.value;
    writeURLState();
    render({ preserveView: true });
  });
  document.getElementById("tool-filter")?.addEventListener("change", (event) => {
    viewFilters.tool = event.target.value;
    writeURLState();
    render({ preserveView: true });
  });
  document.getElementById("status-filter")?.addEventListener("change", (event) => {
    viewFilters.status = event.target.value;
    writeURLState();
    refresh({ forceRender: true });
  });
  document.getElementById("context-filter-reset")?.addEventListener("click", () => {
    const reload = Boolean(viewFilters.status);
    viewFilters = { model: "", tool: "", status: "" };
    writeURLState();
    if (reload) refresh({ forceRender: true });
    else render({ preserveView: true });
  });
  document.getElementById("refresh")?.addEventListener("click", () => refresh());
  document.getElementById("theme-toggle")?.addEventListener("click", () => updateConfig({ ...config, theme: config.theme === "dark" ? "light" : "dark" }));
  document.getElementById("settings-toggle")?.addEventListener("click", () => { settingsOpen = true; render(); });
  document.getElementById("settings-close")?.addEventListener("click", () => { settingsOpen = false; render(); });
  document.getElementById("drawer-backdrop")?.addEventListener("click", () => { settingsOpen = false; render(); });
  document.getElementById("custom-chart-create")?.addEventListener("click", () => {
    settingsOpen = false;
    customBuilder = { open: true, step: 1, chartType: "", dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "", width: "half" };
    render({ preserveView: true });
  });
  const closeCustomBuilder = () => {
    customBuilder = { open: false, step: 1, chartType: "", dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "", width: "half" };
    render({ preserveView: true });
  };
  document.getElementById("custom-builder-close")?.addEventListener("click", closeCustomBuilder);
  document.getElementById("custom-builder-cancel")?.addEventListener("click", closeCustomBuilder);
  document.getElementById("custom-builder-backdrop")?.addEventListener("click", closeCustomBuilder);
  document.querySelectorAll("[data-custom-chart-type]").forEach((button) => button.addEventListener("click", () => {
    customBuilder = { ...customBuilder, step: 2, chartType: button.dataset.customChartType, dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "" };
    render({ preserveView: true });
  }));
  document.getElementById("custom-builder-back")?.addEventListener("click", () => {
    customBuilder = { ...customBuilder, step: 1, dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "" };
    render({ preserveView: true });
  });
  document.querySelectorAll("[data-custom-dimension]").forEach((button) => button.addEventListener("click", () => {
    const dataset = button.dataset.customDataset;
    const dimension = button.dataset.customDimension;
    const sameGroup = customBuilder.dataset === dataset;
    let dimensions = sameGroup ? [...customBuilder.dimensions] : [];
    const dimensionLimit = chartTypeDimensionLimit(customBuilder.chartType);
    if (dimensions.includes(dimension)) {
      if (dimensions.length > 1) dimensions = dimensions.filter((id) => id !== dimension);
    } else if (dimensions.length < dimensionLimit) {
      dimensions.push(dimension);
    }
    const group = dimensionGroupById(dataset);
    const metrics = chartTypeMetrics(dataset, customBuilder.chartType);
    const metric = sameGroup && metrics.some((item) => item.id === customBuilder.metric)
      ? customBuilder.metric
      : metrics[0]?.id || "";
    const slots = Math.min(chartTypeMetricSlots(customBuilder.chartType), Math.max(1, metrics.length));
    const secondaryMetric = slots >= 2 && sameGroup && metrics.some((item) => item.id === customBuilder.secondaryMetric) && customBuilder.secondaryMetric !== metric
      ? customBuilder.secondaryMetric
      : slots >= 2 ? metrics.find((item) => item.id !== metric)?.id || "" : "";
    const sizeMetric = slots >= 3 && sameGroup && metrics.some((item) => item.id === customBuilder.sizeMetric) && ![metric, secondaryMetric].includes(customBuilder.sizeMetric)
      ? customBuilder.sizeMetric
      : slots >= 3 ? metrics.find((item) => ![metric, secondaryMetric].includes(item.id))?.id || "" : "";
    customBuilder = { ...customBuilder, dataset, dimensions, metric, secondaryMetric, sizeMetric, title: "" };
    render({ preserveView: true });
  }));
  document.getElementById("custom-metric")?.addEventListener("change", (event) => {
    const metrics = chartTypeMetrics(customBuilder.dataset, customBuilder.chartType);
    const metric = event.target.value;
    const secondaryMetric = customBuilder.secondaryMetric !== metric
      ? customBuilder.secondaryMetric
      : metrics.find((item) => item.id !== metric)?.id || "";
    const sizeMetric = ![metric, secondaryMetric].includes(customBuilder.sizeMetric)
      ? customBuilder.sizeMetric
      : metrics.find((item) => ![metric, secondaryMetric].includes(item.id))?.id || secondaryMetric;
    customBuilder = { ...customBuilder, metric, secondaryMetric, sizeMetric, title: "" };
    render({ preserveView: true });
  });
  document.getElementById("custom-secondary-metric")?.addEventListener("change", (event) => {
    const metrics = chartTypeMetrics(customBuilder.dataset, customBuilder.chartType);
    const secondaryMetric = event.target.value;
    const sizeMetric = ![customBuilder.metric, secondaryMetric].includes(customBuilder.sizeMetric)
      ? customBuilder.sizeMetric
      : metrics.find((item) => ![customBuilder.metric, secondaryMetric].includes(item.id))?.id || secondaryMetric;
    customBuilder = { ...customBuilder, secondaryMetric, sizeMetric, title: "" };
    render({ preserveView: true });
  });
  document.getElementById("custom-size-metric")?.addEventListener("change", (event) => {
    customBuilder = { ...customBuilder, sizeMetric: event.target.value, title: "" };
    render({ preserveView: true });
  });
  document.getElementById("custom-chart-add")?.addEventListener("click", () => {
    const metric = document.getElementById("custom-metric")?.value || customBuilder.metric;
    const secondaryMetric = document.getElementById("custom-secondary-metric")?.value || customBuilder.secondaryMetric;
    const sizeMetric = document.getElementById("custom-size-metric")?.value || customBuilder.sizeMetric;
    const fallbackTitle = defaultCustomChartTitle(customBuilder.dataset, customBuilder.dimensions, metric);
    const title = document.getElementById("custom-chart-title")?.value.trim() || fallbackTitle;
    const width = document.getElementById("custom-chart-width")?.value === "full" ? "full" : "half";
    const id = `custom-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
    config = saveConfig({
      ...config,
      customCharts: [...config.customCharts, {
        id, title, chartType: customBuilder.chartType, dataset: customBuilder.dataset,
        dimensions: customBuilder.dimensions, metric,
        ...(secondaryMetric ? { secondaryMetric } : {}),
        ...(sizeMetric ? { sizeMetric } : {}),
        width, domain: activeDomain,
      }],
    });
    customBuilder = { open: false, step: 1, chartType: "", dataset: "", dimensions: [], metric: "", secondaryMetric: "", sizeMetric: "", title: "", width: "half" };
    render({ preserveView: true });
  });
  document.querySelectorAll("[data-custom-chart-delete]").forEach((button) => button.addEventListener("click", () => {
    const item = config.customCharts.find((chart) => chart.id === button.dataset.customChartDelete);
    if (!item || !window.confirm(`删除“${item.title}”？`)) return;
    updateConfig({ ...config, customCharts: config.customCharts.filter((chart) => chart.id !== item.id) });
  }));
  document.querySelectorAll("[data-custom-chart-favorite]").forEach((button) => button.addEventListener("click", () => {
    const id = button.dataset.customChartFavorite;
    config = saveConfig({
      ...config,
      customCharts: config.customCharts.map((chart) => chart.id === id ? { ...chart, favorite: !chart.favorite } : chart),
    });
    render({ preserveView: true });
  }));
  document.getElementById("kpi-edit-toggle")?.addEventListener("click", () => { kpiEditorOpen = !kpiEditorOpen; sectionKpiEditor = null; render({ preserveView: true }); });
  document.querySelectorAll("[data-kpi-visible]").forEach((input) => input.addEventListener("change", () => {
    var kpiId = input.dataset.kpiVisible;
    var checked = input.checked;
    var metrics = [...(config.kpiMetrics || [])];
    var existing = metrics.find((m) => m.id === kpiId);
    if (existing) existing.visible = checked;
    else metrics.push({ id: kpiId, visible: checked });
    config = saveConfig({ ...config, kpiMetrics: metrics });
    render({ preserveView: true });
  }));
  // Section KPI editor toggle
  document.querySelectorAll("[data-section-kpi-edit]").forEach((button) => button.addEventListener("click", () => {
    const section = button.dataset.sectionKpiEdit;
    sectionKpiEditor = sectionKpiEditor === section ? null : section;
    kpiEditorOpen = false;
    render({ preserveView: true });
  }));
  // Section KPI checkbox changes (minimum 2 visible)
  document.querySelectorAll("[data-section-kpi-visible]").forEach((input) => input.addEventListener("change", () => {
    const [section, kpiId] = input.dataset.sectionKpiVisible.split(":");
    var sectionMetrics = [...(config.sectionKpis?.[section] || [])];
    var existing = sectionMetrics.find((m) => m.id === kpiId);
    var visibleCount = sectionMetrics.filter((m) => m.visible).length;
    // Prevent unchecking if only 2 visible
    if (existing && existing.visible && !input.checked && visibleCount <= 2) {
      input.checked = true;
      return;
    }
    if (existing) existing.visible = input.checked;
    else sectionMetrics.push({ id: kpiId, visible: input.checked });
    config = saveConfig({ ...config, sectionKpis: { ...config.sectionKpis, [section]: sectionMetrics } });
    render({ preserveView: true });
  }));
  document.getElementById("refresh-interval")?.addEventListener("change", (event) => updateConfig({ ...config, refreshInterval: Number(event.target.value) }));
  document.getElementById("cost-budget")?.addEventListener("change", (event) => updateConfig({
    ...config, thresholds: { ...config.thresholds, costBudgetUsd: Math.max(0, Number(event.target.value) || 0) },
  }));
  document.getElementById("session-stuck-hours")?.addEventListener("change", (event) => updateConfig({
    ...config, thresholds: { ...config.thresholds, sessionStuckMs: Math.max(300000, (Number(event.target.value) || 1) * 3600000) },
  }));
  document.querySelectorAll("[data-alert-ack]").forEach((button) => button.addEventListener("click", () => {
    alertState = saveAlertState({
      ...alertState,
      acknowledged: { ...alertState.acknowledged, [button.dataset.alertAck]: new Date().toISOString() },
    });
    render({ preserveView: true });
  }));
  document.querySelectorAll("[data-alert-silence]").forEach((button) => button.addEventListener("click", () => {
    alertState = saveAlertState({
      ...alertState,
      silenced: { ...alertState.silenced, [button.dataset.alertSilence]: Date.now() + 3600000 },
    });
    render({ preserveView: true });
  }));
  document.getElementById("alert-state-reset")?.addEventListener("click", () => {
    alertState = saveAlertState({ acknowledged: {}, silenced: {} });
    render({ preserveView: true });
  });
  document.querySelectorAll("[data-module-visible]").forEach((input) => input.addEventListener("change", () => updateConfig({ ...config, modules: config.modules.map((m) => m.id === input.dataset.moduleVisible ? { ...m, visible: input.checked } : m) })));
  document.getElementById("config-save")?.addEventListener("click", () => {
    try { updateConfig(JSON.parse(document.getElementById("config-json").value)); settingsOpen = false; render(); }
    catch (reason) { error = `Dashboard JSON 无效：${reason.message}`; render(); }
  });
  document.getElementById("config-reset")?.addEventListener("click", () => { config = resetConfig(); render(); schedule(); });
  document.getElementById("refresh-pricing")?.addEventListener("click", async () => {
    const btn = document.getElementById("refresh-pricing");
    if (btn) { btn.textContent = "获取中…"; btn.disabled = true; }
    try {
      await fetchModelPricing();
      // Reload dashboard to recompute costs with new pricing
      if (btn) btn.textContent = "✓ 已更新，刷新数据中…";
      await refresh({ forceRender: true });
      if (btn) { btn.textContent = "从 OpenRouter 刷新定价"; btn.disabled = false; }
    } catch (err) {
      if (btn) { btn.textContent = "✗ 失败：" + (err.message || "网络错误"); btn.disabled = false; }
      setTimeout(() => { if (btn) btn.textContent = "从 OpenRouter 刷新定价"; }, 3000);
    }
  });
  document.getElementById("session-picker")?.addEventListener("change", async (event) => {
    try { sessionDetail = await loadSession(event.target.value); writeURLState(); render(); }
    catch (reason) { error = reason.message; render(); }
  });
  bindDrag();
}

function bindDrag() {
  let dragged = "";
  document.querySelectorAll("[data-module]").forEach((panel) => {
    panel.addEventListener("dragstart", () => { dragged = panel.dataset.module; dragActive = true; panel.classList.add("dragging"); });
    panel.addEventListener("dragend", () => { dragActive = false; panel.classList.remove("dragging"); markInteraction(); });
    panel.addEventListener("dragover", (event) => event.preventDefault());
    panel.addEventListener("drop", (event) => {
      event.preventDefault(); const target = panel.dataset.module;
      if (!dragged || dragged === target) return;
      const modules = [...config.modules]; const from = modules.findIndex((m) => m.id === dragged); const to = modules.findIndex((m) => m.id === target);
      const [item] = modules.splice(from, 1); modules.splice(to, 0, item); dragged = ""; dragActive = false; updateConfig({ ...config, modules });
    });
  });
  // KPI editor drag reorder
  let kpiDragged = null;
  var kpiList = document.getElementById("kpi-editor-list");
  if (kpiList) {
    kpiList.querySelectorAll(".kpi-check").forEach((item) => {
      item.draggable = true;
      item.addEventListener("dragstart", () => { kpiDragged = item; item.classList.add("dragging"); });
      item.addEventListener("dragend", () => { item.classList.remove("dragging"); kpiDragged = null; });
      item.addEventListener("dragover", (event) => event.preventDefault());
      item.addEventListener("drop", (event) => {
        event.preventDefault();
        if (!kpiDragged || kpiDragged === item) return;
        var fromId = kpiDragged.dataset.kpiId;
        var toId = item.dataset.kpiId;
        var metrics = [...(config.kpiMetrics || [])];
        var fromIdx = metrics.findIndex((m) => m.id === fromId);
        var toIdx = metrics.findIndex((m) => m.id === toId);
        if (fromIdx < 0 || toIdx < 0) return;
        var [moved] = metrics.splice(fromIdx, 1);
        metrics.splice(toIdx, 0, moved);
        config = saveConfig({ ...config, kpiMetrics: metrics });
        render({ preserveView: true });
      });
    });
  }
  let customDragged = "";
  document.querySelectorAll("[data-custom-chart]").forEach((panel) => {
    panel.addEventListener("dragstart", () => { customDragged = panel.dataset.customChart; dragActive = true; panel.classList.add("dragging"); });
    panel.addEventListener("dragend", () => { customDragged = ""; dragActive = false; panel.classList.remove("dragging"); markInteraction(); });
    panel.addEventListener("dragover", (event) => event.preventDefault());
    panel.addEventListener("drop", (event) => {
      event.preventDefault();
      const target = panel.dataset.customChart;
      if (!customDragged || customDragged === target) return;
      const charts = [...config.customCharts];
      const from = charts.findIndex((item) => item.id === customDragged);
      const to = charts.findIndex((item) => item.id === target);
      if (from < 0 || to < 0) return;
      const [item] = charts.splice(from, 1);
      charts.splice(to, 0, item);
      customDragged = "";
      dragActive = false;
      updateConfig({ ...config, customCharts: charts });
    });
  });
}

function connectStream() {
  clearTimeout(streamReconnectTimer);
  if (navigator.onLine === false) return;
  const stream = new EventSource("/api/v1/stream");
  stream.onopen = () => { streamBackoffMs = 1000; };
  stream.addEventListener("monitor-event", () => {
    clearTimeout(streamTimer);
    const refreshFromStream = () => {
      if (interactionActive()) streamTimer = setTimeout(refreshFromStream, 750);
      else refresh({ automatic: true });
    };
    // Debounce multiple rapid SSE events into a single refresh
    streamTimer = setTimeout(refreshFromStream, 1000);
  });
  stream.onerror = () => {
    if (stream.readyState !== EventSource.CLOSED) return;
    stream.close();
    const delay = streamBackoffMs;
    streamBackoffMs = Math.min(30_000, streamBackoffMs * 2);
    streamReconnectTimer = setTimeout(connectStream, delay);
  };
}

bindInteractionGuard();

// --- URL & localStorage state sync ---
function readURLState() {
  const params = new URLSearchParams(location.search);
  const range = params.get("range");
  const instanceId = params.get("instance") || "";
  const agentId = params.get("agent") || "";
  activeDomain = normalizeDomain(params.get("view") || "");
  viewFilters = {
    model: params.get("model") || "",
    tool: params.get("tool") || "",
    status: params.get("status") || "",
  };
  // URL params take priority; fall back to localStorage; finally default
  let useRange = range;
  let useInstance = instanceId;
  let useAgent = agentId;
  try {
    const saved = JSON.parse(localStorage.getItem(FILTER_KEY) || "{}");
    if (!useRange || !["1h", "6h", "24h", "7d", "30d"].includes(useRange)) {
      if (saved.range && ["1h", "6h", "24h", "7d", "30d"].includes(saved.range)) useRange = saved.range;
    }
    if (!useInstance && saved.instanceId) useInstance = saved.instanceId;
    if (!useAgent && saved.agentId) useAgent = saved.agentId;
    if (!params.has("view") && saved.activeDomain) activeDomain = normalizeDomain(saved.activeDomain);
    if (!params.has("model") && saved.viewFilters?.model) viewFilters.model = saved.viewFilters.model;
    if (!params.has("tool") && saved.viewFilters?.tool) viewFilters.tool = saved.viewFilters.tool;
    if (!params.has("status") && saved.viewFilters?.status) viewFilters.status = saved.viewFilters.status;
  } catch {}
  filters = timeFilters(useRange || "24h", useInstance, useAgent);
}

function saveFilters() {
  localStorage.setItem(FILTER_KEY, JSON.stringify({
    range: filters.range, instanceId: filters.instanceId, agentId: filters.agentId,
    activeDomain, viewFilters,
  }));
}

function writeURLState() {
  const params = new URLSearchParams();
  params.set("range", filters.range);
  params.set("view", activeDomain);
  if (filters.instanceId) params.set("instance", filters.instanceId);
  if (filters.agentId) params.set("agent", filters.agentId);
  if (viewFilters.model) params.set("model", viewFilters.model);
  if (viewFilters.tool) params.set("tool", viewFilters.tool);
  if (viewFilters.status) params.set("status", viewFilters.status);
  if (sessionDetail?.sessionId) params.set("session", sessionDetail.sessionId);
  const url = `${location.pathname}?${params}`;
  history.replaceState(null, "", url);
  saveFilters();
}

// Listen for browser back/forward
window.addEventListener("popstate", () => {
  readURLState();
  refresh();
});

window.addEventListener("offline", () => {
  connectionLost = true;
  dataStale = Boolean(data);
  clearTimeout(refreshTimer);
  clearTimeout(streamReconnectTimer);
  render({ preserveView: true });
});

window.addEventListener("online", () => {
  connectionLost = true;
  connectStream();
  refresh({ forceRender: true });
});

readURLState();
lastRenderKey = `${filters.range}|${filters.instanceId}|${filters.agentId}|${viewFilters.status}`;

// Load session from URL if present
const initialSession = new URLSearchParams(location.search).get("session");
if (initialSession) {
  loadSession(initialSession).then((d) => { sessionDetail = d; }).catch(() => {}).finally(() => {
    render();
    refresh();
    requestAnimationFrame(() => scrollDomainNavToActive());
  });
} else {
  render();
  refresh();
  requestAnimationFrame(() => scrollDomainNavToActive());
}
connectStream();
