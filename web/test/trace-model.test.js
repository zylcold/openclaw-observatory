import test from "node:test";
import assert from "node:assert/strict";
import { buildTraceTree, traceSummary } from "../src/trace-model.js";

test("builds nested trace tree and marks the longest critical path", () => {
  const detail = {
    runs: [{ runId: "run", spanId: "root", durationMs: 100 }],
    timeline: [
      { id: "llm", kind: "llm", spanId: "llm", parentSpanId: "root", durationMs: 800 },
      { id: "tool", kind: "tool", spanId: "tool", parentSpanId: "root", durationMs: 100 },
      { id: "child", kind: "mcp", spanId: "child", parentSpanId: "llm", durationMs: 250 },
    ],
  };
  const tree = buildTraceTree(detail);
  assert.equal(tree.roots[0].children.length, 2);
  assert.equal(tree.roots[0].children[0].children[0].id, "child");
  assert.deepEqual([...tree.criticalSpanIds], ["root", "llm", "child"]);
  assert.equal(tree.criticalDurationMs, 1150);
});

test("summarizes safe trace accounting fields", () => {
  const summary = traceSummary({ timeline: [
    { kind: "llm", inputTokens: 10, outputTokens: 5, costUsd: 0.1, status: "completed" },
    { kind: "retry", status: "retried" },
    { kind: "tool", status: "failed" },
  ] });
  assert.deepEqual(summary, { llmCalls: 1, toolCalls: 1, totalTokens: 15, costUsd: 0.1, errors: 1, retries: 1 });
});
