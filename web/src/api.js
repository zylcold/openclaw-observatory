const API = "/api/v1";

import { patchDashboardCosts, getPricing } from "./pricing.js";

function query(params = {}) {
  const q = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== "" && value !== null && value !== undefined) q.set(key, value);
  });
  return q.size ? `?${q}` : "";
}

async function get(path, params) {
  const url = `${API}${path}${query(params)}`;
  let response;
  for (let attempt = 0; attempt < 2; attempt++) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10_000);
    try {
      response = await fetch(url, { headers: { Accept: "application/json" }, signal: controller.signal });
      break;
    } catch (error) {
      if (error?.name !== "AbortError" || attempt === 1) {
        throw error?.name === "AbortError" ? new Error("请求超时（10 秒）") : error;
      }
    } finally {
      clearTimeout(timeout);
    }
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body?.error?.message || `${response.status} ${response.statusText}`);
  if (!Object.prototype.hasOwnProperty.call(body, "data")) {
    throw new Error("Observatory API 返回了无效响应");
  }
  return body.data;
}

export async function loadDashboard(filters) {
  const data = await get("/dashboard", { ...filters, limit: 200 });
  // Patch costs using cached pricing data (computed from token counts)
  patchDashboardCosts(data, getPricing());
  return data;
}

export const loadSession = (sessionId) => get(`/sessions/${encodeURIComponent(sessionId)}`);
