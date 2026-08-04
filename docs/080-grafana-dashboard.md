# Grafana Dashboard

The source-controlled `OpenClaw Observatory Overview` dashboard is uploaded to
the shared remote Grafana at <https://grafana.yunlongzhu.com>. The project does
not deploy a local Grafana. All panels query the daemon's Prometheus contract.

## Layout

1. Active Gateway responsiveness, response time, active sessions/runs, and
   rolling 24-hour tokens/cost.
2. CPU, memory, file descriptors, IO, and disk-free percentage.
3. Gateway response/heartbeat freshness, restarts, dropped events, queue depth,
   and storage query latency.
4. Agent run volume plus token and cost rates by agent.
5. Model request/token volume plus OpenRouter-style hourly token and cost
   stacked bars over the default 24-hour range.
6. Agent × model 24-hour model share, token share, attributed cost, and rates.

The dashboard provides a Prometheus datasource selector plus bounded
multi-select variables for `instance`, `agent`, and `model`. Every panel follows
that remote datasource. Counter queries use Grafana's `$__rate_interval`; the
remote scrape interval is 15 seconds. Agent × model panels are capped at the
top 20 series to limit browser and Prometheus load.

Remote API authentication uses `GRAFANA_TOKEN`. Keep it in the OpenClaw runtime
environment and never commit it. Dashboard UID `openclaw-observatory` is stable,
so API uploads update the existing remote dashboard instead of creating copies.

Canonical attribution counters are
`openclaw_llm_tokens_by_agent_model_total` and
`openclaw_llm_cost_usd_by_agent_model_total`. The original names remain
available as deprecated compatibility aliases.

## OpenRouter cost estimation

OpenClaw events may report `costUsd=0`. Observatory therefore refreshes
OpenRouter's model catalog every six hours and caches it at
`<data-dir>/openrouter-pricing.json` with mode `0600`. For each LLM call:

1. a positive reported cost remains authoritative;
2. otherwise prompt, completion, cache-read, and cache-write tokens are priced
   from the cached OpenRouter catalog;
3. unknown models remain visible through
   `openclaw_llm_unpriced_tokens_total` instead of silently receiving a price.

The effective cumulative cost is exported through
`openclaw_llm_cost_usd_total` and
`openclaw_llm_cost_usd_by_agent_model_total`. Grafana derives 24-hour totals
from the timestamp-accurate `openclaw_llm_cost_usd_24h` and
`openclaw_llm_cost_usd_by_agent_model_24h` gauges; hourly rates use the
cumulative counters with `rate()`.

The public catalog does not require a key in the normal case. If OpenRouter
requires authenticated access, set `OPENROUTER_API_KEY` in the Observatory
daemon environment. Optional daemon flags are `--pricing-url` and
`--pricing-refresh-interval`; setting the interval to `0` disables refresh and
retains fallback/cached prices.

Catalog health is exported as `openclaw_pricing_catalog_models` and
`openclaw_pricing_catalog_last_success_unixtime`.

## Alert defaults

- `OpenClawGatewayDown`: gateway down for 2 minutes;
- `OpenClawGatewayUnresponsive`: active HTTP health probes fail for 30 seconds;
- `OpenClawGatewaySlowResponse`: active health probes exceed 2 seconds for 5 minutes;
- `OpenClawGatewayHeartbeatStale`: no heartbeat for more than 2 minutes;
- `OpenClawObservatoryDown`: Prometheus cannot scrape Observatory for 1 minute;
- `OpenClawHighMemory`: RSS above 2 GiB for 10 minutes;
- `OpenClawHighLLMErrorRate`: error ratio over 10% for 10 minutes;
- `OpenClawToolErrorSpike`: over five errors in five minutes;
- `OpenClawMonitorDroppingEvents`: any drops in ten minutes.

These are initial examples, not universal safe thresholds. Operators must tune
them for model latency, host memory, workload size, and expected tool failures.
