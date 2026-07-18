import { customChart, hasChart, palette, updateChartData } from "../charts.js";
import {
  CHART_TYPES, CHART_TYPE_GROUPS, buildCustomChartSeries, chartTypeById, chartTypeDimensionLimit,
  chartTypeMetricSlots, chartTypeMetrics, defaultCustomChartTitle, dimensionById, dimensionGroupById,
  metricById, suitableDimensionGroupsForDomain,
} from "../custom-chart-model.js";
import { esc } from "../format.js";

const glyphSvg = (content) => `<svg viewBox="0 0 96 48" preserveAspectRatio="xMidYMid meet" aria-hidden="true" focusable="false">${content}</svg>`;

function chartGlyph(type) {
  const axis = `<path class="glyph-grid" d="M10 7V40H88"/>`;
  switch (type) {
    case "line":
      return glyphSvg(`${axis}<path class="glyph-line" d="M14 34L31 25L47 30L65 14L84 19"/>`);
    case "area":
      return glyphSvg(`${axis}<path class="glyph-fill-primary" d="M14 34L31 25L47 30L65 14L84 19V40H14Z"/><path class="glyph-line" d="M14 34L31 25L47 30L65 14L84 19"/>`);
    case "stepLine":
      return glyphSvg(`${axis}<path class="glyph-line" d="M14 34H31V27H48V20H66V12H84"/>`);
    case "cumulativeLine":
      return glyphSvg(`${axis}<path class="glyph-line" d="M14 35L31 32L48 27L66 19L84 10"/>`);
    case "stackedArea":
      return glyphSvg(`${axis}<path class="glyph-fill-secondary" d="M14 35L31 28L48 31L66 20L84 24V40H14Z"/><path class="glyph-fill-primary" d="M14 27L31 19L48 22L66 10L84 15V24L66 20L48 31L31 28L14 35Z"/><path class="glyph-line" d="M14 27L31 19L48 22L66 10L84 15"/>`);
    case "bar":
      return glyphSvg(`${axis}<rect class="glyph-fill-primary" x="17" y="27" width="11" height="13" rx="1"/><rect class="glyph-fill-primary" x="35" y="17" width="11" height="23" rx="1"/><rect class="glyph-fill-primary" x="53" y="23" width="11" height="17" rx="1"/><rect class="glyph-fill-primary" x="71" y="10" width="11" height="30" rx="1"/>`);
    case "horizontalBar":
      return glyphSvg(`<path class="glyph-grid" d="M12 7V41"/><rect class="glyph-fill-primary" x="12" y="9" width="48" height="7" rx="1"/><rect class="glyph-fill-primary" x="12" y="21" width="70" height="7" rx="1"/><rect class="glyph-fill-primary" x="12" y="33" width="57" height="7" rx="1"/>`);
    case "stackedBar":
      return glyphSvg(`${axis}<rect class="glyph-fill-primary" x="18" y="26" width="14" height="14"/><rect class="glyph-fill-secondary" x="18" y="15" width="14" height="11"/><rect class="glyph-fill-primary" x="42" y="20" width="14" height="20"/><rect class="glyph-fill-secondary" x="42" y="9" width="14" height="11"/><rect class="glyph-fill-primary" x="66" y="29" width="14" height="11"/><rect class="glyph-fill-secondary" x="66" y="17" width="14" height="12"/>`);
    case "combo":
      return glyphSvg(`${axis}<rect class="glyph-fill-primary" x="18" y="27" width="11" height="13" rx="1"/><rect class="glyph-fill-primary" x="40" y="19" width="11" height="21" rx="1"/><rect class="glyph-fill-primary" x="62" y="25" width="11" height="15" rx="1"/><path class="glyph-line-secondary" d="M14 31L35 20L57 25L82 11"/>`);
    case "waterfall":
      return glyphSvg(`${axis}<rect class="glyph-fill-primary" x="16" y="28" width="13" height="12" rx="1"/><path class="glyph-grid" d="M29 28H37M50 19H58M71 24H79"/><rect class="glyph-fill-secondary" x="37" y="19" width="13" height="9" rx="1"/><rect class="glyph-fill-primary" x="58" y="24" width="13" height="8" rx="1"/><rect class="glyph-fill-secondary" x="79" y="11" width="9" height="13" rx="1"/>`);
    case "histogram":
      return glyphSvg(`${axis}<rect class="glyph-fill-primary" x="16" y="33" width="10" height="7"/><rect class="glyph-fill-primary" x="26" y="24" width="10" height="16"/><rect class="glyph-fill-primary" x="36" y="11" width="10" height="29"/><rect class="glyph-fill-primary" x="46" y="16" width="10" height="24"/><rect class="glyph-fill-primary" x="56" y="27" width="10" height="13"/><rect class="glyph-fill-primary" x="66" y="32" width="10" height="8"/>`);
    case "doughnut":
      return glyphSvg(`<circle class="glyph-ring-secondary" cx="48" cy="24" r="16" pathLength="100"/><circle class="glyph-ring-primary" cx="48" cy="24" r="16" pathLength="100" stroke-dasharray="62 38" transform="rotate(-90 48 24)"/>`);
    case "pie":
      return glyphSvg(`<circle class="glyph-fill-secondary" cx="48" cy="24" r="18"/><path class="glyph-fill-primary" d="M48 24V6A18 18 0 0 0 32.4 33Z"/><path class="glyph-fill-tertiary" d="M48 24H66A18 18 0 0 1 32.4 33Z"/>`);
    case "polarArea":
      return glyphSvg(`<circle class="glyph-grid" cx="48" cy="24" r="19"/><path class="glyph-fill-primary" d="M48 24V7A17 17 0 0 1 65 24Z"/><path class="glyph-fill-secondary" d="M48 24H63A15 15 0 0 1 48 39Z"/><path class="glyph-fill-tertiary" d="M48 24V35A11 11 0 0 1 37 24Z"/><path class="glyph-grid" d="M48 5V43M29 24H67"/>`);
    case "radar":
      return glyphSvg(`<path class="glyph-grid" d="M48 5L70 17L66 40H30L26 17ZM48 5V40M26 17L66 40M70 17L30 40"/><path class="glyph-fill-primary" d="M48 11L64 19L59 34H35L31 20Z"/>`);
    case "gauge":
      return glyphSvg(`<path class="glyph-ring-secondary" d="M24 39A24 24 0 0 1 72 39"/><path class="glyph-ring-primary" d="M24 39A24 24 0 0 1 62 19"/><path class="glyph-line" d="M48 39L63 20"/>`);
    case "scatter":
    case "bubble":
      return glyphSvg(`${axis}<circle class="glyph-fill-primary" cx="26" cy="31" r="${type === "bubble" ? 6 : 3}"/><circle class="glyph-fill-secondary" cx="48" cy="23" r="${type === "bubble" ? 9 : 3}"/><circle class="glyph-fill-tertiary" cx="73" cy="13" r="${type === "bubble" ? 5 : 3}"/>`);
    default:
      return glyphSvg("");
  }
}

export function customChartBuilderHTML(builder, domain = "overview") {
  if (!builder?.open) return "";
  const step = builder.step === 2 ? 2 : 1;
  const group = dimensionGroupById(builder.dataset);
  const dimensions = Array.isArray(builder.dimensions) ? builder.dimensions : [];
  const selectedMetric = metricById(builder.dataset, builder.metric);
  const selectedSecondaryMetric = metricById(builder.dataset, builder.secondaryMetric);
  const selectedSizeMetric = metricById(builder.dataset, builder.sizeMetric);
  const availableMetrics = group ? chartTypeMetrics(group.id, builder.chartType) : [];
  const metricSlots = Math.min(chartTypeMetricSlots(builder.chartType), Math.max(1, availableMetrics.length));
  const dimensionLimit = chartTypeDimensionLimit(builder.chartType);
  const suggestedTitle = defaultCustomChartTitle(builder.dataset, dimensions, builder.metric);
  const suitableGroups = suitableDimensionGroupsForDomain(builder.chartType, domain);
  return `
    <div class="modal-backdrop open" id="custom-builder-backdrop"></div>
    <section class="chart-builder" role="dialog" aria-modal="true" aria-labelledby="custom-builder-title">
      <header class="chart-builder-header">
        <div><span class="eyebrow">自定义表盘</span><h2 id="custom-builder-title">${step === 1 ? "选择图表" : "选择维度"}</h2></div>
        <button id="custom-builder-close" class="modal-close" aria-label="关闭">×</button>
      </header>
      <div class="builder-progress" aria-label="创建进度">
        <span class="active"><b>1</b>选择图表</span><i></i><span class="${step === 2 ? "active" : ""}"><b>2</b>选择维度</span>
      </div>
      ${step === 1 ? `
        <div class="chart-type-groups">
          ${CHART_TYPE_GROUPS.map((typeGroup) => `<section class="chart-type-group">
            <header><div><strong>${esc(typeGroup.label)}</strong><small>${esc(typeGroup.description)}</small></div><span>${CHART_TYPES.filter((item) => item.group === typeGroup.id).length} 种</span></header>
            <div class="chart-type-grid">${CHART_TYPES.filter((item) => item.group === typeGroup.id).map((item) => {
              const supported = suitableDimensionGroupsForDomain(item.id, domain).length > 0;
              return `<button class="chart-type-card" data-custom-chart-type="${item.id}" ${supported ? "" : "disabled"}>
              <span class="chart-glyph">${chartGlyph(item.id)}</span>
              <strong>${esc(item.label)}</strong><small>${esc(supported ? item.description : "当前观测域指标不足")}</small>
            </button>`;
            }).join("")}</div>
          </section>`).join("")}
        </div>
      ` : `
        <div class="builder-selection">
          <span class="selection-preview">${chartGlyph(builder.chartType)}</span>
          <div><small>已选图表</small><strong>${esc(chartTypeById(builder.chartType)?.label || "")}</strong></div>
          <button id="custom-builder-back">更换</button>
        </div>
        <div class="builder-section">
          <label class="builder-label">数据维度 <small>已按当前观测域分组，${dimensionLimit === 1 ? "仅可选择 1 个维度" : `可选择 1–${dimensionLimit} 个维度`}</small></label>
          <div class="dimension-groups">
            ${suitableGroups.map((item) => {
              const groupSelected = builder.dataset === item.id;
              return `<section class="dimension-group ${groupSelected ? "selected" : ""}">
                <header><div><strong>${esc(item.label)}</strong><small>${esc(item.description)}</small></div>${groupSelected ? `<span>${dimensions.length}/${dimensionLimit}</span>` : ""}</header>
                <div class="dimension-grid">${item.dimensions.map((dim) => {
                  const selected = groupSelected && dimensions.includes(dim.id);
                  const disabled = groupSelected && dimensions.length >= dimensionLimit && !selected;
                  return `<button class="dimension-card ${selected ? "selected" : ""}" data-custom-dataset="${item.id}" data-custom-dimension="${dim.id}" ${disabled ? "disabled" : ""}>
                    <i>${selected ? dimensions.indexOf(dim.id) + 1 : ""}</i><strong>${esc(dim.label)}</strong>
                  </button>`;
                }).join("")}</div>
              </section>`;
            }).join("")}
          </div>
        </div>
        ${group && dimensions.length ? `
          <div class="builder-fields builder-metric-fields">
            <label>${metricSlots >= 2 ? "X / 柱指标" : "指标"}<select id="custom-metric">${availableMetrics.map((item) => `<option value="${item.id}" ${builder.metric === item.id ? "selected" : ""}>${esc(item.label)}${item.unit ? ` (${esc(item.unit)})` : ""}</option>`).join("")}</select></label>
            ${metricSlots >= 2 ? `<label>Y / 线指标<select id="custom-secondary-metric">${availableMetrics.filter((item) => item.id !== builder.metric).map((item) => `<option value="${item.id}" ${builder.secondaryMetric === item.id ? "selected" : ""}>${esc(item.label)}${item.unit ? ` (${esc(item.unit)})` : ""}</option>`).join("")}</select></label>` : ""}
            ${metricSlots >= 3 ? `<label>气泡大小<select id="custom-size-metric">${availableMetrics.filter((item) => ![builder.metric, builder.secondaryMetric].includes(item.id)).map((item) => `<option value="${item.id}" ${builder.sizeMetric === item.id ? "selected" : ""}>${esc(item.label)}${item.unit ? ` (${esc(item.unit)})` : ""}</option>`).join("")}</select></label>` : ""}
          </div>
          <div class="builder-fields builder-display-fields">
            <label>表盘名称<input id="custom-chart-title" maxlength="80" value="${esc(builder.title || suggestedTitle)}" placeholder="${esc(suggestedTitle)}"></label>
            <label>面板宽度<select id="custom-chart-width"><option value="half" ${builder.width !== "full" ? "selected" : ""}>半宽</option><option value="full" ${builder.width === "full" ? "selected" : ""}>全宽</option></select></label>
          </div>
          <div class="builder-summary"><span>${dimensions.map((id) => esc(dimensionById(group.id, id)?.label)).join(" × ")}</span><i></i><span>${[selectedMetric?.label, selectedSecondaryMetric?.label, selectedSizeMetric?.label].filter(Boolean).map(esc).join(" / ")}</span></div>
        ` : `<div class="builder-empty">请从一个分组中${dimensionLimit === 1 ? "选择 1 个维度" : `选择 1–${dimensionLimit} 个维度`}。</div>`}
      `}
      <footer class="chart-builder-actions">
        <button id="custom-builder-cancel">取消</button>
        ${step === 2 ? `<button id="custom-chart-add" class="primary" ${group && dimensions.length ? "" : "disabled"}>添加到仪表盘</button>` : ""}
      </footer>
    </section>`;
}

export function customChartPanelHTML(item, data) {
  const series = buildCustomChartSeries(data, item);
  const chartType = chartTypeById(item.chartType);
  const group = dimensionGroupById(item.dataset);
  const metricLabels = [item.metric, item.secondaryMetric, item.sizeMetric].map((id) => metricById(item.dataset, id)?.label).filter(Boolean).join(" / ");
  const dimensionLabels = item.dimensions.map((id) => dimensionById(item.dataset, id)?.label).filter(Boolean).join(" × ");
  const canvas = series.labels.length
    ? `<div class="chart custom-chart chart-${esc(item.chartType)} ${item.chartType === "horizontalBar" ? "horizontal" : ""}"><canvas id="custom-chart-${item.id}"></canvas></div>`
    : `<div class="empty">当前筛选范围内没有可用于此表盘的数据</div>`;
  return `<article class="panel module-custom ${item.width === "full" ? "custom-full" : ""}" draggable="true" data-custom-chart="${esc(item.id)}">
    <header><div><span class="drag" title="拖拽排序">⠿</span><h2>${esc(item.title)}</h2><span class="panel-meta">${esc(chartType?.label || "")} · ${esc(group?.label || "")} / ${esc(dimensionLabels)} · ${esc(metricLabels)}</span></div>
      <button class="panel-delete" data-custom-chart-delete="${esc(item.id)}" title="删除表盘" aria-label="删除 ${esc(item.title)}">删除</button>
    </header>${canvas}
  </article>`;
}

function chartConfig(item, series) {
  let chartDatasets = series.datasets.map((dataset, index) => ({
    ...dataset,
    label: series.unit ? `${dataset.label} (${series.unit})` : dataset.label,
    borderColor: palette[index % palette.length],
    backgroundColor: palette[index % palette.length] + "99",
    borderWidth: 2,
    pointRadius: ["line", "area", "stepLine", "cumulativeLine", "stackedArea"].includes(item.chartType) ? 1.5 : ["scatter", "bubble"].includes(item.chartType) ? 4 : 0,
    borderRadius: ["bar", "horizontalBar", "stackedBar", "waterfall", "histogram"].includes(item.chartType) ? 5 : 0,
  }));
  const base = {
    labels: series.labels,
    datasets: chartDatasets,
    options: { scales: { y: { beginAtZero: true } } },
  };
  if (item.chartType === "area" || item.chartType === "stackedArea") {
    base.datasets.forEach((dataset, index) => {
      dataset.fill = true;
      dataset.backgroundColor = palette[index % palette.length] + "2b";
    });
  }
  if (item.chartType === "stepLine") base.datasets.forEach((dataset) => { dataset.stepped = "before"; });
  if (item.chartType === "cumulativeLine") {
    base.datasets.forEach((dataset) => {
      let total = 0;
      dataset.data = dataset.data.map((value) => (total += Number(value || 0)));
    });
  }
  if (item.chartType === "stackedArea" || item.chartType === "stackedBar") {
    base.options.scales = {
      x: { stacked: true, grid: { display: false } },
      y: { stacked: true, beginAtZero: true },
    };
  }
  if (item.chartType === "horizontalBar") {
    base.options.indexAxis = "y";
    base.options.scales = { x: { beginAtZero: true }, y: { grid: { display: false } } };
  }
  if (item.chartType === "combo") {
    const secondary = (series.secondaryDatasets || []).map((dataset, index) => ({
      ...dataset,
      type: "line",
      label: `${dataset.label} · ${series.secondaryLabel}${series.secondaryUnit ? ` (${series.secondaryUnit})` : ""}`,
      borderColor: palette[(index + chartDatasets.length) % palette.length],
      backgroundColor: palette[(index + chartDatasets.length) % palette.length] + "22",
      borderWidth: 2,
      pointRadius: 2,
      tension: 0.3,
      yAxisID: "y1",
    }));
    base.datasets.forEach((dataset) => { dataset.type = "bar"; dataset.yAxisID = "y"; });
    base.datasets.push(...secondary);
    base.options = {
      interaction: { mode: "index", intersect: false },
      scales: {
        x: { grid: { display: false } },
        y: { beginAtZero: true, position: "left", title: { display: true, text: series.label } },
        y1: { beginAtZero: true, position: "right", grid: { drawOnChartArea: false }, title: { display: true, text: series.secondaryLabel } },
      },
    };
  }
  if (item.chartType === "waterfall") {
    const totals = series.labels.map((_, index) => series.datasets.reduce((sum, dataset) => sum + Number(dataset.data[index] || 0), 0));
    let running = 0;
    base.datasets = [{
      label: series.unit ? `${series.label} (${series.unit})` : series.label,
      data: totals.map((value) => {
        const start = running;
        running += value;
        return [start, running];
      }),
      backgroundColor: totals.map((value) => value < 0 ? palette[4] : palette[0]),
      borderWidth: 0,
      borderRadius: 5,
    }];
    base.options = { scales: { x: { grid: { display: false } }, y: { beginAtZero: true } }, plugins: { legend: { display: false } } };
  }
  if (item.chartType === "histogram") {
    const values = series.datasets.flatMap((dataset) => dataset.data).map(Number).filter(Number.isFinite);
    const min = Math.min(...values), max = Math.max(...values);
    const binCount = Math.min(10, Math.max(4, Math.ceil(Math.sqrt(values.length || 1))));
    const width = max > min ? (max - min) / binCount : 1;
    const bins = Array(binCount).fill(0);
    values.forEach((value) => { bins[Math.min(binCount - 1, Math.max(0, Math.floor((value - min) / width)))]++; });
    base.labels = bins.map((_, index) => `${(min + index * width).toFixed(1)}–${(min + (index + 1) * width).toFixed(1)}`);
    base.datasets = [{ label: "频次", data: bins, backgroundColor: palette[1] + "b8", borderWidth: 0, borderRadius: 3, barPercentage: 1, categoryPercentage: 1 }];
    base.options = { scales: { x: { grid: { display: false }, ticks: { maxRotation: 45 } }, y: { beginAtZero: true, ticks: { precision: 0 } } }, plugins: { legend: { display: false } } };
  }
  if (item.chartType === "gauge") {
    const values = series.datasets.flatMap((dataset) => dataset.data).map(Number).filter(Number.isFinite);
    const value = Math.max(0, Math.min(100, values.reduce((sum, item) => sum + item, 0) / Math.max(1, values.length)));
    base.labels = [series.label, "剩余"];
    base.datasets = [{ label: series.label, data: [value, 100 - value], backgroundColor: [value >= 85 ? palette[0] : value >= 60 ? palette[3] : palette[4], "rgba(128,140,160,.18)"], borderWidth: 0, circumference: 180, rotation: -90 }];
    base.options = { circumference: 180, rotation: -90, cutout: "74%", plugins: { legend: { display: false }, tooltip: { filter: (context) => context.dataIndex === 0 } } };
  }
  if (item.chartType === "scatter" || item.chartType === "bubble") {
    base.datasets.forEach((dataset, index) => {
      dataset.backgroundColor = palette[index % palette.length] + "99";
      dataset.borderColor = palette[index % palette.length];
      dataset.pointRadius = item.chartType === "scatter" ? 4 : undefined;
    });
    base.options = {
      parsing: false,
      layout: { padding: 12 },
      scales: {
        x: { title: { display: true, text: `${series.label}${series.unit ? ` (${series.unit})` : ""}` } },
        y: { title: { display: true, text: `${series.secondaryLabel}${series.secondaryUnit ? ` (${series.secondaryUnit})` : ""}` } },
      },
      plugins: {
        tooltip: {
          callbacks: {
            title: (items) => items[0]?.raw?.category || "",
            label: (context) => `${context.dataset.label}: ${context.raw.x.toFixed(2)}, ${context.raw.y.toFixed(2)}${context.raw.r ? `, ${series.sizeLabel} ${context.raw.r.toFixed(1)}` : ""}`,
          },
        },
      },
    };
  }
  if (["doughnut", "pie", "polarArea"].includes(item.chartType)) {
    base.datasets.forEach((dataset) => {
      dataset.backgroundColor = series.labels.map((_, index) => palette[index % palette.length]);
      dataset.borderWidth = 0;
    });
    base.options = { plugins: { legend: { position: "bottom" } } };
    if (item.chartType === "doughnut") base.options.cutout = "64%";
  }
  if (item.chartType === "radar") {
    base.datasets.forEach((dataset, index) => {
      dataset.backgroundColor = palette[index % palette.length] + "2b";
      dataset.pointBackgroundColor = palette[index % palette.length];
      dataset.pointRadius = 2;
    });
    base.options = { scales: { r: { beginAtZero: true } } };
  }
  return base;
}

export function paintCustomCharts(data, items = []) {
  for (const item of items) {
    const series = buildCustomChartSeries(data, item);
    if (!series.labels.length) continue;
    const config = chartConfig(item, series);
    customChart(`custom-chart-${item.id}`, item.chartType, { labels: config.labels, datasets: config.datasets }, config.options);
  }
}

export function updateCustomCharts(data, items = []) {
  for (const item of items) {
    if (["cumulativeLine", "combo", "waterfall", "histogram", "gauge", "scatter", "bubble"].includes(item.chartType)) return false;
    const series = buildCustomChartSeries(data, item);
    const id = `custom-chart-${item.id}`;
    if (!series.labels.length) {
      if (hasChart(id)) updateChartData(id, [], []);
      continue;
    }
    if (!hasChart(id)) return false;
    updateChartData(id, series.labels, series.datasets.map((dataset) => ({
      label: series.unit ? `${dataset.label} (${series.unit})` : dataset.label,
      data: dataset.data,
    })));
  }
  return true;
}
