import test from "node:test";
import assert from "node:assert/strict";

import { normalizeConfig } from "../src/config.js";
import {
  CHART_TYPES, CHART_TYPE_GROUPS, DEFAULT_CUSTOM_CHARTS, buildCustomChartSeries,
  chartTypeDimensionLimit, chartTypeMetrics, normalizeCustomCharts, suitableDimensionGroups,
  suitableDimensionGroupsForDomain,
} from "../src/custom-chart-model.js";

test("new and migrated configs receive default custom charts", () => {
  const config = normalizeConfig({});
  assert.equal(config.customCharts.length, DEFAULT_CUSTOM_CHARTS.length);
  assert.deepEqual(config.customCharts.find((chart) => chart.id === "default-model-tokens").dimensions, ["time", "model"]);
  assert.equal(new Set(config.customCharts.map((chart) => chart.domain)).size, 7);
});

test("an explicitly empty custom chart list remains empty", () => {
  assert.deepEqual(normalizeConfig({ customCharts: [] }).customCharts, []);
});

test("favorites persist only for charts outside Overview", () => {
  const charts = normalizeCustomCharts([
    { id: "model-favorite", chartType: "bar", dataset: "models", dimensions: ["model"], metric: "requests", favorite: true },
    { id: "overview-favorite", chartType: "line", dataset: "overview", dimensions: ["time"], metric: "runs", favorite: true },
  ]);
  assert.equal(charts[0].favorite, true);
  assert.equal("favorite" in charts[1], false);
});

test("invalid charts and dimensions are discarded", () => {
  const charts = normalizeCustomCharts([
    { id: "bad-type", chartType: "unknown", dataset: "models", dimensions: ["model"], metric: "requests" },
    { id: "bad-dimension", chartType: "bar", dataset: "models", dimensions: ["tool"], metric: "requests" },
    { id: "ok", chartType: "bar", dataset: "models", dimensions: ["provider", "model", "time"], metric: "requests" },
  ]);
  assert.equal(charts.length, 1);
  assert.deepEqual(charts[0].dimensions, ["provider", "model"]);
});

test("two dimensions produce category and series datasets", () => {
  const data = {
    timeseries: {
      models: [
        { time: "2026-07-18T00:00:00Z", provider: "openai", model: "gpt-5", inputTokens: 10 },
        { time: "2026-07-18T00:00:00Z", provider: "anthropic", model: "claude", inputTokens: 20 },
        { time: "2026-07-18T01:00:00Z", provider: "openai", model: "gpt-5", inputTokens: 30 },
      ],
    },
  };
  const series = buildCustomChartSeries(data, {
    chartType: "area",
    dataset: "models",
    dimensions: ["time", "model"],
    metric: "totalTokens",
  });
  assert.equal(series.labels.length, 2);
  assert.deepEqual(series.datasets, [
    { label: "gpt-5", data: [10, 30] },
    { label: "claude", data: [20, 0] },
  ]);
});

test("chart choices expose only suitable dimension groups", () => {
  assert.deepEqual(suitableDimensionGroups("line").map((group) => group.id), ["overview", "agents", "sessions", "models", "tools", "infrastructure", "errors"]);
  assert.deepEqual(suitableDimensionGroups("doughnut").map((group) => group.id), ["agents", "sessions", "models", "tools", "errors"]);
  assert.deepEqual(suitableDimensionGroupsForDomain("line", "tools").map((group) => group.id), ["tools"]);
});

test("chart library exposes 18 types in four semantic groups", () => {
  assert.equal(CHART_TYPES.length, 18);
  assert.deepEqual(CHART_TYPE_GROUPS.map((group) => group.id), ["trend", "compare", "distribution", "relationship"]);
  assert.ok(["stepLine", "cumulativeLine", "stackedArea", "stackedBar", "combo", "waterfall", "histogram", "gauge", "scatter", "bubble"]
    .every((id) => CHART_TYPES.some((item) => item.id === id)));
});

test("chart-specific rules constrain dimensions and metrics", () => {
  assert.equal(chartTypeDimensionLimit("gauge"), 1);
  assert.deepEqual(chartTypeMetrics("models", "gauge").map((item) => item.id), ["errorRate"]);
  assert.ok(chartTypeMetrics("models", "scatter").length > 2);
  assert.deepEqual(suitableDimensionGroupsForDomain("bubble", "errors"), []);
});

test("dual and triple metric charts normalize and build relationship points", () => {
  const charts = normalizeCustomCharts([
    { id: "missing-y", chartType: "scatter", dataset: "models", dimensions: ["model"], metric: "costUsd" },
    { id: "scatter", chartType: "scatter", dataset: "models", dimensions: ["model"], metric: "costUsd", secondaryMetric: "averageDurationMs" },
    { id: "bubble", chartType: "bubble", dataset: "models", dimensions: ["model"], metric: "costUsd", secondaryMetric: "averageDurationMs", sizeMetric: "requests" },
  ]);
  assert.equal(charts.length, 2);
  assert.equal(charts[1].sizeMetric, "requests");
  const series = buildCustomChartSeries({ models: [
    { model: "gpt", costUsd: 2, averageDurationMs: 500, requests: 9 },
    { model: "claude", costUsd: 3, averageDurationMs: 800, requests: 16 },
  ] }, charts[1]);
  assert.deepEqual(series.datasets[0].data.map(({ x, y, category }) => ({ x, y, category })), [
    { x: 3, y: 800, category: "claude" },
    { x: 2, y: 500, category: "gpt" },
  ]);
  assert.ok(series.datasets[0].data.every((point) => point.r >= 4));
});

test("gauge rejects non-percentage metrics", () => {
  const charts = normalizeCustomCharts([
    { id: "bad", chartType: "gauge", dataset: "models", dimensions: ["model"], metric: "costUsd" },
    { id: "ok", chartType: "gauge", dataset: "models", dimensions: ["model", "provider"], metric: "errorRate" },
  ]);
  assert.equal(charts.length, 1);
  assert.deepEqual(charts[0].dimensions, ["model"]);
});
