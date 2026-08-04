import test from "node:test";
import assert from "node:assert/strict";

import { customChartBuilderHTML, customChartPanelHTML } from "../src/components/custom-charts.js";

test("chart picker renders every glyph with the same preserved aspect ratio", () => {
  const html = customChartBuilderHTML({ open: true, step: 1 }, "models");
  assert.equal((html.match(/data-custom-chart-type=/g) || []).length, 18);
  assert.equal((html.match(/viewBox="0 0 96 48"/g) || []).length, 18);
  assert.equal((html.match(/preserveAspectRatio="xMidYMid meet"/g) || []).length, 18);
});

test("source charts and Overview copies expose the correct favorite actions", () => {
  const chart = {
    id: "model-cost",
    title: "模型成本",
    chartType: "bar",
    dataset: "models",
    dimensions: ["model"],
    metric: "costUsd",
    domain: "models",
    favorite: true,
    width: "half",
  };
  const data = { models: [{ model: "gpt-5", costUsd: 2 }] };
  const source = customChartPanelHTML(chart, data);
  const favorite = customChartPanelHTML(chart, data, { favoriteView: true, sourceLabel: "Model" });
  assert.match(source, /data-custom-chart-favorite="model-cost"/);
  assert.match(source, /data-custom-chart-delete="model-cost"/);
  assert.match(source, /aria-pressed="true"/);
  assert.match(favorite, /data-favorite-chart="model-cost"/);
  assert.match(favorite, /来自 Model/);
  assert.doesNotMatch(favorite, /data-custom-chart-delete=/);
  assert.doesNotMatch(favorite, /draggable="true"/);
});
