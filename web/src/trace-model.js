function duration(item) {
  const explicit = Number(item?.durationMs);
  if (Number.isFinite(explicit) && explicit >= 0) return explicit;
  const start = Date.parse(item?.startedAt);
  const end = Date.parse(item?.endedAt);
  return Number.isFinite(start) && Number.isFinite(end) ? Math.max(0, end - start) : 0;
}

export function traceSummary(detail) {
  const stored = detail?.summary || {};
  const timeline = detail?.timeline || [];
  const llm = timeline.filter((item) => item.kind === "llm");
  const tools = timeline.filter((item) => item.kind === "tool" || item.kind === "mcp");
  return {
    llmCalls: Number(stored.llmCalls ?? llm.length),
    toolCalls: Number(stored.toolCalls ?? tools.length),
    totalTokens: Number(stored.totalTokens ?? llm.reduce((sum, item) => sum
      + Number(item.inputTokens || 0) + Number(item.outputTokens || 0)
      + Number(item.cacheReadTokens || 0) + Number(item.cacheWriteTokens || 0), 0)),
    costUsd: Number(stored.costUsd ?? llm.reduce((sum, item) => sum + Number(item.costUsd || 0), 0)),
    errors: Number(stored.errors ?? timeline.filter((item) => item.status === "failed").length),
    retries: Number(stored.retries ?? timeline.filter((item) => item.kind === "retry").length),
  };
}

export function buildTraceTree(detail) {
  const runs = detail?.runs || [];
  const items = (detail?.timeline || []).map((item) => ({
    ...item,
    spanId: item.spanId || item.id,
    parentSpanId: item.parentSpanId || item.runId || "",
    durationMs: duration(item),
    children: [],
  }));
  const bySpan = new Map(items.map((item) => [item.spanId, item]));
  const roots = runs.map((run) => ({
    ...run,
    kind: "run",
    id: run.runId,
    label: run.model || run.agentId || run.runId,
    spanId: run.spanId || run.runId,
    parentSpanId: run.parentSpanId || "",
    durationMs: duration(run),
    children: [],
  }));
  for (const root of roots) bySpan.set(root.spanId, root);

  const orphans = [];
  for (const item of items) {
    const parent = bySpan.get(item.parentSpanId);
    if (parent && parent !== item) parent.children.push(item);
    else {
      const run = roots.find((root) => root.runId === item.runId);
      if (run) run.children.push(item);
      else orphans.push(item);
    }
  }
  if (orphans.length) {
    roots.push({
      kind: "trace", id: "unlinked", label: "Unlinked spans", spanId: "unlinked",
      durationMs: Math.max(0, ...orphans.map((item) => item.durationMs)), children: orphans,
    });
  }

  const criticalSpanIds = new Set();
  const visit = (node, seen = new Set()) => {
    if (seen.has(node.spanId)) return { score: 0, path: [] };
    const nextSeen = new Set(seen).add(node.spanId);
    let childBest = { score: 0, path: [] };
    for (const child of node.children || []) {
      const candidate = visit(child, nextSeen);
      if (candidate.score > childBest.score) childBest = candidate;
    }
    return { score: Number(node.durationMs || 0) + childBest.score, path: [node.spanId, ...childBest.path] };
  };
  let best = { score: 0, path: [] };
  for (const root of roots) {
    const candidate = visit(root);
    if (candidate.score > best.score) best = candidate;
  }
  best.path.forEach((id) => criticalSpanIds.add(id));
  return { roots, criticalSpanIds, criticalDurationMs: best.score };
}
