/**
 * Model pricing module.
 *
 * Fetches pricing from OpenRouter's public /models endpoint, caches in
 * localStorage, and computes cost based on token counts.
 *
 * Pricing is per-token (USD).  OpenRouter returns prices per single token
 * (e.g. 0.00000035 = $0.35 per million tokens).
 */

const PRICING_KEY = "openclaw-observatory-pricing-v1";
const PRICING_TS_KEY = "openclaw-observatory-pricing-ts";
const PRICING_URL = "https://openrouter.ai/api/v1/models";

/**
 * Provider/model name normalisation map.
 * Maps observatory provider|model → OpenRouter model id.
 */
const MODEL_ALIASES = {
  // bailian (Alibaba / Qwen)
  "bailian|qwen3.7-plus":        "qwen/qwen3.7-plus",
  "bailian|qwen3-coder-plus":    "qwen/qwen3-coder-plus",
  "bailian|qwen3-coder-next":    "qwen/qwen3-coder-next",
  "bailian|qwen3-max":           "qwen/qwen3-max",
  // zai (Zhipu / GLM)
  "zai|glm-5.2":                 "z-ai/glm-5.2",
  "zai|glm-5-turbo":             "z-ai/glm-5-turbo",
  "zai|glm-5":                   "z-ai/glm-5",
  // openai
  "openai|gpt-4o":               "openai/gpt-4o",
  "openai|gpt-4o-mini":          "openai/gpt-4o-mini",
  "openai|o1":                   "openai/o1",
  "openai|o3-mini":              "openai/o3-mini",
  // anthropic
  "anthropic|claude-3.5-sonnet": "anthropic/claude-3.5-sonnet",
  "anthropic|claude-3-5-haiku":  "anthropic/claude-3-5-haiku",
  // deepseek
  "deepseek|deepseek-v4-pro":    "deepseek/deepseek-v4-pro",
  "deepseek|deepseek-v4-flash":  "deepseek/deepseek-v4-flash",
};

/**
 * Fallback hardcoded pricing for models that might not be on OpenRouter.
 * Prices are per single token (USD).
 */
const FALLBACK_PRICING = {
  "bailian|qwen3.7-plus":        { input: 0.00000032,  output: 0.00000128 },
  "bailian|qwen3-coder-plus":    { input: 0.00000035,  output: 0.0000011 },
  "bailian|qwen3-coder-next":    { input: 0.00000011,  output: 0.0000008 },
  "bailian|qwen3-max":           { input: 0.00000125,  output: 0.00000375 },
  "zai|glm-5.2":                 { input: 0.00000035,  output: 0.0000011 },
  "zai|glm-5-turbo":             { input: 0.0000012,   output: 0.000004 },
  "zai|glm-5":                   { input: 0.0000006,   output: 0.00000192 },
};

function loadCached() {
  try {
    const raw = localStorage.getItem(PRICING_KEY);
    if (!raw) return {};
    return JSON.parse(raw);
  } catch { return {}; }
}

function saveCached(pricing) {
  localStorage.setItem(PRICING_KEY, JSON.stringify(pricing));
  localStorage.setItem(PRICING_TS_KEY, String(Date.now()));
}

export function getPricingTimestamp() {
  const ts = Number(localStorage.getItem(PRICING_TS_KEY) || 0);
  return ts ? new Date(ts) : null;
}

/**
 * Fetch model pricing from OpenRouter and cache it.
 * Returns the normalised pricing map: { "provider|model": { input, output } }
 */
export async function fetchModelPricing() {
  const response = await fetch(PRICING_URL);
  if (!response.ok) throw new Error(`OpenRouter API ${response.status}`);
  const body = await response.json();
  const models = body?.data || [];
  const pricing = {};

  for (const model of models) {
    const id = model.id;
    if (!id || !id.includes("/")) continue;
    const [provider, ...modelParts] = id.split("/");
    const modelName = modelParts.join("/");
    const p = model.pricing || {};
    const inputPerToken = Number(p.prompt);
    const outputPerToken = Number(p.completion);
    if (!isFinite(inputPerToken) || !isFinite(outputPerToken)) continue;
    if (inputPerToken === 0 && outputPerToken === 0) continue;

    // Normalise provider names to match observatory conventions
    const normProvider = normaliseProvider(provider);
    const key = `${normProvider}|${modelName.toLowerCase()}`;
    pricing[key] = { input: inputPerToken, output: outputPerToken };
  }

  // Merge fallback pricing (for models missing from OpenRouter)
  for (const [key, val] of Object.entries(FALLBACK_PRICING)) {
    if (!pricing[key]) pricing[key] = val;
  }

  saveCached(pricing);
  return pricing;
}

function normaliseProvider(openRouterProvider) {
  const map = {
    "qwen": "bailian",
    "alibaba": "bailian",
    "z-ai": "zai",
    "zhipu": "zai",
  };
  return map[openRouterProvider.toLowerCase()] || openRouterProvider.toLowerCase();
}

/**
 * Get the current pricing map (cached). If cache is empty, returns fallback pricing.
 */
export function getPricing() {
  const cached = loadCached();
  if (Object.keys(cached).length > 0) return cached;
  return { ...FALLBACK_PRICING };
}

/**
 * Resolve the pricing for a specific provider|model combination.
 * Tries alias map first, then direct lookup.
 */
export function resolvePricing(provider, model, pricing = getPricing()) {
  const key = `${(provider || "").toLowerCase()}|${(model || "").toLowerCase()}`;
  // Direct match
  if (pricing[key]) return pricing[key];
  // Alias match
  if (MODEL_ALIASES[key] && pricing[MODEL_ALIASES[key]]) return pricing[MODEL_ALIASES[key]];
  // Try OpenRouter id format via alias
  const aliasVal = MODEL_ALIASES[key];
  if (aliasVal) {
    const aliasKey = aliasVal.toLowerCase();
    if (pricing[aliasKey]) return pricing[aliasKey];
  }
  return null;
}

/**
 * Compute cost for a single LLM call record.
 */
export function computeCallCost(call, pricing = getPricing()) {
  const p = resolvePricing(call.provider, call.model, pricing);
  if (!p) return 0;
  const inputTokens = Number(call.inputTokens || 0) + Number(call.cacheReadTokens || 0);
  const outputTokens = Number(call.outputTokens || 0);
  // Cache write tokens are typically charged at input rate or free depending on provider
  const cacheWriteTokens = Number(call.cacheWriteTokens || 0);
  return inputTokens * p.input + outputTokens * p.output + cacheWriteTokens * p.input;
}

/**
 * Patch cost values in dashboard data.
 * Mutates the data object in-place, replacing costUsd with computed values.
 */
export function patchDashboardCosts(data, pricing = getPricing()) {
  if (!data) return data;

  // Patch LLM calls
  if (Array.isArray(data.llmCalls)) {
    for (const call of data.llmCalls) {
      if (!call.costUsd || call.costUsd === 0) {
        call.costUsd = computeCallCost(call, pricing);
      }
    }
  }

  // Patch model stats — recompute from token sums
  if (Array.isArray(data.models)) {
    for (const m of data.models) {
      const p = resolvePricing(m.provider, m.model, pricing);
      if (p) {
        const inputT = Number(m.inputTokens || 0) + Number(m.cacheReadTokens || 0);
        const outputT = Number(m.outputTokens || 0);
        const cacheWriteT = Number(m.cacheWriteTokens || 0);
        m.costUsd = inputT * p.input + outputT * p.output + cacheWriteT * p.input;
      }
    }
  }

  // Patch agent stats — recompute from LLM data or sum model costs per agent
  if (Array.isArray(data.agents) && Array.isArray(data.models)) {
    // Agent stats already have token breakdowns, compute cost
    for (const a of data.agents) {
      const p = resolvePricing(a.provider || a.agentId, a.model, pricing);
      // Agent stats may not have provider/model, so we compute from sum of llmCalls
      // We skip agent-level patching here and rely on the model/call level patches
    }
  }

  // Patch cost trends
  if (Array.isArray(data.costTrends)) {
    for (const row of data.costTrends) {
      const p = resolvePricing(row.provider, row.model, pricing);
      if (p) {
        const inputT = Number(row.inputTokens || 0);
        const outputT = Number(row.outputTokens || 0);
        row.costUsd = inputT * p.input + outputT * p.output;
      }
    }
  }

  // Patch cost summary — recompute totals
  if (data.costSummary && Array.isArray(data.llmCalls)) {
    let total = 0;
    for (const call of data.llmCalls) {
      total += computeCallCost(call, pricing);
    }
    data.costSummary.totalCost = total;
    // Keep other summary fields as-is (they'll be 0 if backend doesn't provide)
  }

  return data;
}
