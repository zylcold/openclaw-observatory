import test from "node:test";
import assert from "node:assert/strict";

import { normalizeConfig } from "../src/config.js";
import {
  DEFAULT_CUSTOM_CHARTS, buildCustomChartSeries, normalizeCustomCharts, suitableDimensionGroups,
} from "../src/custom-chart-model.js";

test("new and migrated configs receive default custom charts", () => {
  const config = normalizeConfig({});
  assert.equal(config.customCharts.length, DEFAULT_CUSTOM_CHARTS.length);
  assert.deepEqual(config.customCharts[2].dimensions, ["time", "model"]);
});

test("an explicitly empty custom chart list remains empty", () => {
  assert.deepEqual(normalizeConfig({ customCharts: [] }).customCharts, []);
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
  assert.deepEqual(suitableDimensionGroups("line").map((group) => group.id), ["overview", "agents", "models"]);
  assert.deepEqual(suitableDimensionGroups("doughnut").map((group) => group.id), ["agents", "models", "tools", "errors"]);
});
