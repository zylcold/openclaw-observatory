const BASE_URL = "http://127.0.0.1:10086";

const paths = {
  status: "/api/v1/status",
  instances: "/api/v1/instances",
  sessions: "/api/v1/sessions",
  runs: "/api/v1/runs",
  resources: "/api/v1/resources",
  tools: "/api/v1/tools/stats",
  models: "/api/v1/models/stats",
  events: "/api/v1/events",
};

export function buildQueryURL(params) {
  const resource = params?.resource;
  let path;
  if (resource === "session" || resource === "run") {
    if (typeof params.id !== "string" || !params.id) throw new Error(`id is required for ${resource} detail`);
    path = `/api/v1/${resource === "session" ? "sessions" : "runs"}/${encodeURIComponent(params.id)}`;
  } else {
    path = paths[resource];
  }
  if (!path) throw new Error("unsupported Observatory resource");
  const url = new URL(path, BASE_URL);
  if (!["status", "instances", "session", "run", "tools", "models"].includes(resource)) {
    const limit = Math.min(100, Math.max(1, Number(params.limit) || 20));
    url.searchParams.set("limit", String(limit));
  }
  for (const key of ["instanceId", "status", "eventType", "from", "to"]) {
    if (typeof params[key] === "string" && params[key]) url.searchParams.set(key, params[key]);
  }
  return url;
}

export async function queryObservatory(params, fetchImpl = fetch) {
  const url = buildQueryURL(params);
  const response = await fetchImpl(url, { method: "GET", signal: AbortSignal.timeout(3000), headers: { accept: "application/json" } });
  if (!response.ok) throw new Error(`Observatory returned HTTP ${response.status}`);
  const result = await response.json();
  const encoded = JSON.stringify(result);
  if (encoded.length > 64_000) throw new Error("Observatory response exceeded the tool safety limit; narrow the query");
  return result;
}
