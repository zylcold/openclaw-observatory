import "./styles.css";
import { loadDashboard, loadSession } from "./api.js";
import { loadConfig, resetConfig, saveConfig } from "./config.js";
import { destroyCharts, setChartAnimation } from "./charts.js";
import { timeFilters } from "./state.js";
import { paintCharts, updateCharts, updateNonChartDOM, updateAgentTable } from "./components/modules.js";
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
let interactionTimer = null;
let interactionUntil = 0;
let pointerActive = false;
let dragActive = false;
let openSelect = null;
let deferredRender = false;
let hasRenderedData = false;

const INTERACTION_IDLE_MS = 600;

function interactionActive() {
  return settingsOpen || pointerActive || dragActive || Boolean(openSelect?.isConnected) || Date.now() < interactionUntil;
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

function render({ preserveView = false, deferWhileInteracting = false } = {}) {
  if (deferWhileInteracting && interactionActive()) {
    deferredRender = true;
    markInteraction();
    return;
  }
  deferredRender = false;
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

/**
 * Incrementally update charts and KPI text without full re-render.
 * Falls back to full render if charts don't exist yet.
 */
function incrementalUpdate() {
  if (!data || !hasRenderedData) return false;
  const app = document.getElementById("app");
  if (!app) return false;
  const ok = updateCharts(data);
  if (!ok) return false;
  updateNonChartDOM(app, data, config);
  updateAgentTable(app, data);
  return true;
}

async function refresh({ keepRange = false, automatic = false } = {}) {
  if (loading) return;
  if (automatic && interactionActive()) {
    schedule(750);
    return;
  }
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
    // For background refreshes, try incremental update first.
    // Only do full re-render on first load, error, or explicit user action.
    if (background && !error && hasRenderedData) {
      if (!incrementalUpdate()) {
        render({ preserveView: true, deferWhileInteracting: true });
      }
    } else {
      render({ preserveView: background, deferWhileInteracting: background });
    }
    schedule();
  }
}

function schedule(delay = config.refreshInterval) {
  clearTimeout(refreshTimer);
  if (config.refreshInterval > 0) refreshTimer = setTimeout(() => refresh({ automatic: true }), delay);
}

function updateConfig(next) {
  config = saveConfig(next);
  render();
  schedule();
}

function bind() {
  document.querySelectorAll("[data-range]").forEach((button) => button.addEventListener("click", () => {
    filters = timeFilters(button.dataset.range, filters.instanceId, filters.agentId); writeURLState(); refresh({ keepRange: true });
  }));
  document.getElementById("instance-filter")?.addEventListener("change", (event) => { filters.instanceId = event.target.value; writeURLState(); refresh(); });
  document.getElementById("agent-filter")?.addEventListener("change", (event) => { filters.agentId = event.target.value; writeURLState(); refresh(); });
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
}

function connectStream() {
  const stream = new EventSource("/api/v1/stream");
  stream.addEventListener("monitor-event", () => {
    clearTimeout(streamTimer);
    const refreshFromStream = () => {
      if (interactionActive()) streamTimer = setTimeout(refreshFromStream, 750);
      else refresh({ automatic: true });
    };
    // Debounce multiple rapid SSE events into a single refresh
    streamTimer = setTimeout(refreshFromStream, 1000);
  });
  stream.onerror = () => { stream.close(); setTimeout(connectStream, 5000); };
}

bindInteractionGuard();

// --- URL state sync ---
function readURLState() {
  const params = new URLSearchParams(location.search);
  const range = params.get("range");
  const instanceId = params.get("instance") || "";
  const agentId = params.get("agent") || "";
  if (range && ["1h", "6h", "24h", "7d", "30d"].includes(range)) {
    filters = timeFilters(range, instanceId, agentId);
  } else {
    filters = timeFilters("24h", instanceId, agentId);
  }
}

function writeURLState() {
  const params = new URLSearchParams();
  params.set("range", filters.range);
  if (filters.instanceId) params.set("instance", filters.instanceId);
  if (filters.agentId) params.set("agent", filters.agentId);
  if (sessionDetail?.sessionId) params.set("session", sessionDetail.sessionId);
  const url = `${location.pathname}?${params}`;
  history.replaceState(null, "", url);
}

// Listen for browser back/forward
window.addEventListener("popstate", () => {
  readURLState();
  refresh();
});

readURLState();

// Load session from URL if present
const initialSession = new URLSearchParams(location.search).get("session");
if (initialSession) {
  loadSession(initialSession).then((d) => { sessionDetail = d; }).catch(() => {}).finally(() => {
    render();
    refresh();
  });
} else {
  render();
  refresh();
}
connectStream();
