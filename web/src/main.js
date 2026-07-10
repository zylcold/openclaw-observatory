import "./styles.css";
import { loadDashboard, loadSession } from "./api.js";
import { loadConfig, resetConfig, saveConfig } from "./config.js";
import { destroyCharts, setChartAnimation } from "./charts.js";
import { timeFilters } from "./state.js";
import { paintCharts } from "./components/modules.js";
import { shell } from "./components/shell.js";

const app = document.getElementById("app");
let config = loadConfig();
let filters = timeFilters("24h");
let data = null;
let sessionDetail = null;
let loading = false;
let error = "";
let settingsOpen = false;
let refreshTimer = null;
let streamTimer = null;
let hasRenderedData = false;

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

function render({ preserveView = false } = {}) {
  const view = preserveView ? captureView() : null;
  destroyCharts();
  setChartAnimation(!preserveView && !hasRenderedData);
  document.documentElement.dataset.theme = config.theme;
  app.innerHTML = shell({ config, data, filters, loading, error, settingsOpen, sessionDetail });
  const interval = document.getElementById("refresh-interval");
  if (interval) interval.value = String(config.refreshInterval);
  bind();
  if (data) requestAnimationFrame(() => {
    paintCharts(data);
    restoreView(view);
    hasRenderedData = true;
  });
}

async function refresh({ keepRange = false } = {}) {
  if (loading) return;
  const background = Boolean(data);
  loading = true; error = "";
  if (!keepRange) filters = timeFilters(filters.range, filters.instanceId, filters.agentId);
  if (!background) render();
  else {
    document.documentElement.classList.add("refreshing");
    const button = document.getElementById("refresh");
    if (button) button.textContent = "刷新中…";
  }
  try {
    data = await loadDashboard(filters);
    const exists = data.sessions.some((s) => s.sessionId === sessionDetail?.sessionId);
    if (!exists) sessionDetail = data.sessions[0] ? await loadSession(data.sessions[0].sessionId) : null;
  } catch (reason) {
    error = reason instanceof Error ? reason.message : String(reason);
  } finally {
    loading = false;
    document.documentElement.classList.remove("refreshing");
    render({ preserveView: background });
    schedule();
  }
}

function schedule() {
  clearTimeout(refreshTimer);
  if (config.refreshInterval > 0) refreshTimer = setTimeout(() => {
    if (settingsOpen) schedule();
    else refresh();
  }, config.refreshInterval);
}

function updateConfig(next) {
  config = saveConfig(next);
  render();
  schedule();
}

function bind() {
  document.querySelectorAll("[data-range]").forEach((button) => button.addEventListener("click", () => {
    filters = timeFilters(button.dataset.range, filters.instanceId, filters.agentId); refresh({ keepRange: true });
  }));
  document.getElementById("instance-filter")?.addEventListener("change", (event) => { filters.instanceId = event.target.value; refresh(); });
  document.getElementById("agent-filter")?.addEventListener("change", (event) => { filters.agentId = event.target.value; refresh(); });
  document.getElementById("refresh")?.addEventListener("click", () => refresh());
  document.getElementById("theme-toggle")?.addEventListener("click", () => updateConfig({ ...config, theme: config.theme === "dark" ? "light" : "dark" }));
  document.getElementById("settings-toggle")?.addEventListener("click", () => { settingsOpen = true; render(); });
  document.getElementById("settings-close")?.addEventListener("click", () => { settingsOpen = false; render(); });
  document.getElementById("drawer-backdrop")?.addEventListener("click", () => { settingsOpen = false; render(); });
  document.getElementById("refresh-interval")?.addEventListener("change", (event) => updateConfig({ ...config, refreshInterval: Number(event.target.value) }));
  document.querySelectorAll("[data-module-visible]").forEach((input) => input.addEventListener("change", () => updateConfig({ ...config, modules: config.modules.map((m) => m.id === input.dataset.moduleVisible ? { ...m, visible: input.checked } : m) })));
  document.getElementById("config-save")?.addEventListener("click", () => {
    try { updateConfig(JSON.parse(document.getElementById("config-json").value)); settingsOpen = false; render(); }
    catch (reason) { error = `Dashboard JSON 无效：${reason.message}`; render(); }
  });
  document.getElementById("config-reset")?.addEventListener("click", () => { config = resetConfig(); render(); schedule(); });
  document.getElementById("session-picker")?.addEventListener("change", async (event) => {
    try { sessionDetail = await loadSession(event.target.value); render(); }
    catch (reason) { error = reason.message; render(); }
  });
  bindDrag();
}

function bindDrag() {
  let dragged = "";
  document.querySelectorAll("[data-module]").forEach((panel) => {
    panel.addEventListener("dragstart", () => { dragged = panel.dataset.module; panel.classList.add("dragging"); });
    panel.addEventListener("dragend", () => panel.classList.remove("dragging"));
    panel.addEventListener("dragover", (event) => event.preventDefault());
    panel.addEventListener("drop", (event) => {
      event.preventDefault(); const target = panel.dataset.module;
      if (!dragged || dragged === target) return;
      const modules = [...config.modules]; const from = modules.findIndex((m) => m.id === dragged); const to = modules.findIndex((m) => m.id === target);
      const [item] = modules.splice(from, 1); modules.splice(to, 0, item); dragged = ""; updateConfig({ ...config, modules });
    });
  });
}

function connectStream() {
  const stream = new EventSource("/api/v1/stream");
  stream.addEventListener("monitor-event", () => {
    clearTimeout(streamTimer);
    streamTimer = setTimeout(() => { if (!settingsOpen) refresh(); }, 800);
  });
  stream.onerror = () => { stream.close(); setTimeout(connectStream, 5000); };
}

render();
refresh();
connectStream();
