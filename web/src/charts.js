import {
  Chart, ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Filler,
  Legend, LinearScale, LineController, LineElement, PieController, PointElement,
  PolarAreaController, RadarController, RadialLinearScale, ScatterController, Tooltip,
} from "chart.js";

Chart.register(
  ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Filler, Legend,
  LinearScale, LineController, LineElement, PieController, PointElement, PolarAreaController,
  RadarController, RadialLinearScale, ScatterController, Tooltip,
);
Chart.defaults.font.family = "Inter, ui-sans-serif, system-ui, -apple-system, sans-serif";
Chart.defaults.animation.duration = 250;
Chart.defaults.responsive = true;
Chart.defaults.maintainAspectRatio = false;
Chart.defaults.elements.line.tension = 0.34;
Chart.defaults.elements.line.cubicInterpolationMode = "monotone";
Chart.defaults.elements.line.borderJoinStyle = "round";
Chart.defaults.elements.line.borderCapStyle = "round";

const active = new Map();
export const palette = ["#26d6a2", "#5b9dff", "#ad7cff", "#ffb454", "#ff647c", "#43c4e8", "#88cc62", "#e989d2"];

export function setChartAnimation(enabled) {
  Chart.defaults.animation.duration = enabled ? 250 : 0;
}

export function destroyCharts() {
  active.forEach((chart) => chart.destroy());
  active.clear();
}

function colors() {
  const style = getComputedStyle(document.documentElement);
  return { text: style.getPropertyValue("--muted").trim(), grid: style.getPropertyValue("--grid").trim() };
}

function make(id, config) {
  const canvas = document.getElementById(id);
  if (!canvas) return;
  const c = colors();
  const options = config.options || {};
  options.plugins = { legend: { labels: { color: c.text, boxWidth: 10, usePointStyle: true } }, tooltip: { intersect: false }, ...(options.plugins || {}) };
  options.scales = Object.fromEntries(Object.entries(options.scales || {}).map(([key, scale]) => {
    const isX = key === "x";
    const merged = { grid: { color: c.grid }, ticks: { color: c.text, maxTicksLimit: isX ? 10 : 8 }, ...scale };
    if (merged.ticks) merged.ticks.maxTicksLimit = merged.ticks.maxTicksLimit || (isX ? 10 : 8);
    return [key, merged];
  }));
  active.set(id, new Chart(canvas, { ...config, options }));
}

export const lineChart = (id, labels, datasets, options = {}) => make(id, { type: "line", data: { labels, datasets }, options: { interaction: { mode: "index", intersect: false }, ...options } });
export const comboChart = (id, labels, datasets, options = {}) => make(id, { type: "bar", data: { labels, datasets }, options: { interaction: { mode: "index", intersect: false }, ...options } });
export const doughnutChart = (id, labels, values) => make(id, { type: "doughnut", data: { labels, datasets: [{ data: values, backgroundColor: palette, borderWidth: 0 }] }, options: { cutout: "68%", plugins: { legend: { position: "bottom" } } } });
export const scatterChart = (id, datasets) => make(id, { type: "scatter", data: { datasets }, options: { parsing: false, scales: { x: { title: { display: true, text: "Token" } }, y: { title: { display: true, text: "延迟 ms" } } } } });
export const customChart = (id, requestedType, data, options = {}) => {
  const type = requestedType === "area" ? "line" : requestedType === "horizontalBar" ? "bar" : requestedType;
  return make(id, { type, data, options });
};

// --- Incremental update helpers (no destroy/re-create flicker) ---

/** Update an existing line/bar/combo chart's data in-place. */
export function updateChartData(id, labels, datasets) {
  const chart = active.get(id);
  if (!chart) return false;
  chart.data.labels = labels;
  // Update existing datasets in-place to preserve chart instance state.
  for (let i = 0; i < datasets.length; i++) {
    if (chart.data.datasets[i]) {
      chart.data.datasets[i].data = datasets[i].data;
      // Preserve visual config (borderColor, backgroundColor, etc.) — only data changes.
      if (datasets[i].label !== undefined) chart.data.datasets[i].label = datasets[i].label;
    } else {
      chart.data.datasets.push(datasets[i]);
    }
  }
  // Trim extra datasets if fewer than before.
  while (chart.data.datasets.length > datasets.length) chart.data.datasets.pop();
  chart.update("none"); // skip animation for smooth real-time feel
  return true;
}

/** Update a doughnut chart's data in-place. */
export function updateDoughnut(id, labels, values) {
  const chart = active.get(id);
  if (!chart) return false;
  chart.data.labels = labels;
  chart.data.datasets[0].data = values;
  chart.update("none");
  return true;
}

/** Update a scatter chart's datasets in-place. */
export function updateScatter(id, datasets) {
  const chart = active.get(id);
  if (!chart) return false;
  // Sync dataset count
  for (let i = 0; i < datasets.length; i++) {
    if (chart.data.datasets[i]) {
      chart.data.datasets[i].data = datasets[i].data;
      if (datasets[i].label !== undefined) chart.data.datasets[i].label = datasets[i].label;
    } else {
      chart.data.datasets.push(datasets[i]);
    }
  }
  while (chart.data.datasets.length > datasets.length) chart.data.datasets.pop();
  chart.update("none");
  return true;
}

/** Check if a chart with the given id exists. */
export function hasChart(id) {
  return active.has(id);
}

/** Toggle animation for all charts. */
export function setAllChartAnimation(enabled) {
  Chart.defaults.animation.duration = enabled ? 250 : 0;
}
