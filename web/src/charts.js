import {
  Chart, ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Filler,
  Legend, LinearScale, LineController, LineElement, PointElement, ScatterController, Tooltip,
} from "chart.js";

Chart.register(ArcElement, BarController, BarElement, CategoryScale, DoughnutController, Filler, Legend, LinearScale, LineController, LineElement, PointElement, ScatterController, Tooltip);
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
  options.scales = Object.fromEntries(Object.entries(options.scales || {}).map(([key, scale]) => [key, { grid: { color: c.grid }, ticks: { color: c.text, maxTicksLimit: 8 }, ...scale }]));
  active.set(id, new Chart(canvas, { ...config, options }));
}

export const lineChart = (id, labels, datasets, options = {}) => make(id, { type: "line", data: { labels, datasets }, options: { interaction: { mode: "index", intersect: false }, ...options } });
export const comboChart = (id, labels, datasets, options = {}) => make(id, { type: "bar", data: { labels, datasets }, options: { interaction: { mode: "index", intersect: false }, ...options } });
export const doughnutChart = (id, labels, values) => make(id, { type: "doughnut", data: { labels, datasets: [{ data: values, backgroundColor: palette, borderWidth: 0 }] }, options: { cutout: "68%", plugins: { legend: { position: "bottom" } } } });
export const scatterChart = (id, datasets) => make(id, { type: "scatter", data: { datasets }, options: { parsing: false, scales: { x: { title: { display: true, text: "Token" } }, y: { title: { display: true, text: "延迟 ms" } } } } });
