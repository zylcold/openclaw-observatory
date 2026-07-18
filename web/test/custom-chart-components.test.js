import test from "node:test";
import assert from "node:assert/strict";

import { customChartBuilderHTML } from "../src/components/custom-charts.js";

test("chart picker renders every glyph with the same preserved aspect ratio", () => {
  const html = customChartBuilderHTML({ open: true, step: 1 }, "models");
  assert.equal((html.match(/data-custom-chart-type=/g) || []).length, 18);
  assert.equal((html.match(/viewBox="0 0 96 48"/g) || []).length, 18);
  assert.equal((html.match(/preserveAspectRatio="xMidYMid meet"/g) || []).length, 18);
});
