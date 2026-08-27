const API = "/api/v1";

import { patchDashboardCosts, getPricing } from "./pricing.js";

function query(params = {}) {
  const q = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== "" && value !== null && value !== undefined) q.set(key, value);
  });
  return q.size ? `?${q}` : "";
}

async function get(path, params = {}, { timeoutMs = 10_000, attempts = 2 } = {}) {
  const url = `${API}${path}${query(params)}`;
  let lastError;
  for (let attempt = 0; attempt < attempts; attempt++) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    let response;
    try {
      response = await fetch(url, { headers: { Accept: "application/json" }, signal: controller.signal });
    } catch (error) {
      clearTimeout(timeout);
      if (error?.name === "AbortError") {
        lastError = new Error(`请求超时（${Math.ceil(timeoutMs / 1000)} 秒）`);
        if (attempt + 1 < attempts) continue;
        throw lastError;
      }
      throw error;
    }
    clearTimeout(timeout);

    // Read body as text first, then parse — avoids silent .json() catch
    const text = await response.text();
    let body;
    try {
      body = JSON.parse(text);
    } catch {
      // JSON parse failed (truncated body, connection dropped mid-response, etc.)
      if (attempt + 1 < attempts) {
        lastError = new Error("响应解析失败，正在重试…");
        continue;
      }
      throw new Error("Observatory API 响应解析失败（可能网络中断）");
    }

    if (!response.ok) {
      throw new Error(body?.error?.message || `${response.status} ${response.statusText}`);
    }
    if (!Object.prototype.hasOwnProperty.call(body, "data")) {
      throw new Error("Observatory API 返回了无效响应");
    }
    return body.data;
  }
  throw lastError || new Error("请求失败");
}

export async function loadDashboard(filters) {
  // Snapshot requests return a bounded, cached response. An immediate retry
  // would only create another client wait while the server is already merging it.
  const data = await get("/dashboard/snapshot", { ...filters, limit: 200 }, { timeoutMs: 8_000, attempts: 1 });
  // Patch costs using cached pricing data (computed from token counts)
  patchDashboardCosts(data, getPricing());
  return data;
}

export const loadSummary = () => get("/summary", {}, { timeoutMs: 750, attempts: 1 });

// Memory metrics are served from in-memory state (plus the tiny embedding_calls
// cache), so this endpoint is safe to poll at the normal dashboard cadence.
export const loadMemory = () => get("/memory", {}, { timeoutMs: 3_000, attempts: 1 });

export const loadSession = (sessionId) => get(`/sessions/${encodeURIComponent(sessionId)}`);
