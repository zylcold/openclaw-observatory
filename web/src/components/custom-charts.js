import { customChart, hasChart, palette, updateChartData } from "../charts.js";
import {
  CHART_TYPES, buildCustomChartSeries, chartTypeById, defaultCustomChartTitle,
  dimensionById, dimensionGroupById, metricById, suitableDimensionGroups,
} from "../custom-chart-model.js";
import { esc } from "../format.js";

function chartGlyph(type) {
  if (type === "line" || type === "area") {
    return `<svg viewBox="0 0 88 44" aria-hidden="true"><path class="glyph-grid" d="M5 37H83M5 8V37"/><path class="${type === "area" ? "glyph-area" : "glyph-line"}" d="M7 33L24 24L39 28L56 12L80 17"/></svg>`;
  }
  if (type === "bar" || type === "horizontalBar") {
    return type === "bar"
      ? `<svg viewBox="0 0 88 44" aria-hidden="true"><path class="glyph-grid" d="M5 38H83"/><rect x="12" y="25" width="12" height="13"/><rect x="31" y="15" width="12" height="23"/><rect x="50" y="21" width="12" height="17"/><rect x="69" y="8" width="12" height="30"/></svg>`
      : `<svg viewBox="0 0 88 44" aria-hidden="true"><rect x="8" y="7" width="48" height="6"/><rect x="8" y="19" width="72" height="6"/><rect x="8" y="31" width="58" height="6"/></svg>`;
  }
  if (type === "radar") {
    return `<svg viewBox="0 0 88 44" aria-hidden="true"><path class="glyph-grid" d="M44 4L78 19L66 40H22L10 19ZM44 4V35M10 19L66 40M78 19L22 40"/><path class="glyph-area" d="M44 10L68 21L58 34H29L19 21Z"/></svg>`;
  }
  if (type === "polarArea") {
    return `<svg viewBox="0 0 88 44" aria-hidden="true"><path class="glyph-grid" d="M44 4V40M24 9L64 35M64 9L24 35"/><path class="glyph-area" d="M44 6A18 18 0 0 1 62 24H44ZM44 24H27A17 17 0 0 1 44 7Z"/></svg>`;
  }
  const hole = type === "doughnut" ? `<circle class="glyph-hole" cx="44" cy="22" r="8"/>` : "";
  return `<svg viewBox="0 0 88 44" aria-hidden="true"><path d="M42 3A19 19 0 0 0 25 31L42 22Z"/><path class="glyph-secondary" d="M46 3V20H65A19 19 0 0 0 46 3Z"/><path class="glyph-tertiary" d="M64 24H47L31 34A19 19 0 0 0 64 24Z"/>${hole}</svg>`;
}

export function customChartBuilderHTML(builder) {
  if (!builder?.open) return "";
  const step = builder.step === 2 ? 2 : 1;
  const group = dimensionGroupById(builder.dataset);
  const dimensions = Array.isArray(builder.dimensions) ? builder.dimensions : [];
  const selectedMetric = metricById(builder.dataset, builder.metric);
  const suggestedTitle = defaultCustomChartTitle(builder.dataset, dimensions, builder.metric);
  const suitableGroups = suitableDimensionGroups(builder.chartType);
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
        <div class="chart-type-grid">
          ${CHART_TYPES.map((item) => `<button class="chart-type-card" data-custom-chart-type="${item.id}">
            <span class="chart-glyph">${chartGlyph(item.id)}</span>
            <strong>${esc(item.label)}</strong><small>${esc(item.description)}</small>
          </button>`).join("")}
        </div>
      ` : `
        <div class="builder-selection">
          <span class="selection-preview">${chartGlyph(builder.chartType)}</span>
          <div><small>已选图表</small><strong>${esc(chartTypeById(builder.chartType)?.label || "")}</strong></div>
          <button id="custom-builder-back">更换</button>
        </div>
        <div class="builder-section">
          <label class="builder-label">数据维度 <small>在同一分组内选择 1–2 个维度</small></label>
          <div class="dimension-groups">
            ${suitableGroups.map((item) => {
              const groupSelected = builder.dataset === item.id;
              return `<section class="dimension-group ${groupSelected ? "selected" : ""}">
                <header><div><strong>${esc(item.label)}</strong><small>${esc(item.description)}</small></div>${groupSelected ? `<span>${dimensions.length}/2</span>` : ""}</header>
                <div class="dimension-grid">${item.dimensions.map((dim) => {
                  const selected = groupSelected && dimensions.includes(dim.id);
                  const disabled = groupSelected && dimensions.length >= 2 && !selected;
                  return `<button class="dimension-card ${selected ? "selected" : ""}" data-custom-dataset="${item.id}" data-custom-dimension="${dim.id}" ${disabled ? "disabled" : ""}>
                    <i>${selected ? dimensions.indexOf(dim.id) + 1 : ""}</i><strong>${esc(dim.label)}</strong>
                  </button>`;
                }).join("")}</div>
              </section>`;
            }).join("")}
          </div>
        </div>
        ${group && dimensions.length ? `
          <div class="builder-fields">
            <label>指标<select id="custom-metric">${group.metrics.map((item) => `<option value="${item.id}" ${builder.metric === item.id ? "selected" : ""}>${esc(item.label)}${item.unit ? ` (${esc(item.unit)})` : ""}</option>`).join("")}</select></label>
            <label>表盘名称<input id="custom-chart-title" maxlength="80" value="${esc(builder.title || suggestedTitle)}" placeholder="${esc(suggestedTitle)}"></label>
            <label>面板宽度<select id="custom-chart-width"><option value="half" ${builder.width !== "full" ? "selected" : ""}>半宽</option><option value="full" ${builder.width === "full" ? "selected" : ""}>全宽</option></select></label>
          </div>
          <div class="builder-summary"><span>${dimensions.map((id) => esc(dimensionById(group.id, id)?.label)).join(" × ")}</span><i></i><span>${esc(selectedMetric?.label || group.metrics[0]?.label)}</span></div>
        ` : `<div class="builder-empty">请从一个分组中选择 1–2 个维度。</div>`}
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
  const dimensionLabels = item.dimensions.map((id) => dimensionById(item.dataset, id)?.label).filter(Boolean).join(" × ");
  const canvas = series.labels.length
    ? `<div class="chart custom-chart ${item.chartType === "horizontalBar" ? "horizontal" : ""}"><canvas id="custom-chart-${item.id}"></canvas></div>`
    : `<div class="empty">当前筛选范围内没有可用于此表盘的数据</div>`;
  return `<article class="panel module-custom ${item.width === "full" ? "custom-full" : ""}" draggable="true" data-custom-chart="${esc(item.id)}">
    <header><div><span class="drag" title="拖拽排序">⠿</span><h2>${esc(item.title)}</h2><span class="panel-meta">${esc(chartType?.label || "")} · ${esc(group?.label || "")} / ${esc(dimensionLabels)}</span></div>
      <button class="panel-delete" data-custom-chart-delete="${esc(item.id)}" title="删除表盘" aria-label="删除 ${esc(item.title)}">删除</button>
    </header>${canvas}
  </article>`;
}

function chartConfig(item, series) {
  const chartDatasets = series.datasets.map((dataset, index) => ({
    ...dataset,
    label: series.unit ? `${dataset.label} (${series.unit})` : dataset.label,
    borderColor: palette[index % palette.length],
    backgroundColor: palette[index % palette.length] + "99",
    borderWidth: 2,
    pointRadius: item.chartType === "line" || item.chartType === "area" ? 1.5 : 0,
    borderRadius: item.chartType === "bar" || item.chartType === "horizontalBar" ? 5 : 0,
  }));
  const base = {
    labels: series.labels,
    datasets: chartDatasets,
    options: { scales: { y: { beginAtZero: true } } },
  };
  if (item.chartType === "area") {
    base.datasets.forEach((dataset, index) => {
      dataset.fill = true;
      dataset.backgroundColor = palette[index % palette.length] + "2b";
    });
  }
  if (item.chartType === "horizontalBar") {
    base.options.indexAxis = "y";
    base.options.scales = { x: { beginAtZero: true }, y: { grid: { display: false } } };
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
