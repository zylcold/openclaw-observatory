# Grafana Dashboard

The provisioned `OpenClaw Observatory Overview` dashboard is intentionally
minimal but importable. All panels query the daemon's Prometheus contract.

## Layout

1. Gateway status, uptime, active sessions/runs, today's tokens and cost.
2. CPU, RSS, virtual memory, threads, file descriptors, and IO.
3. LLM requests, input/output tokens, latency percentiles, errors, providers,
   and models.
4. Agent run volume/duration, Tool calls/errors, slow tools, queue depth.
5. Gateway restarts, dropped events, monitor queue depth, and recent alerts.

The MVP JSON focuses on the critical status and volume panels. Later iterations
must preserve stable metric queries, add variables for bounded `instance`,
`provider`, and `model`, and test provisioning on the pinned Grafana image.

## Alert defaults

- `OpenClawGatewayDown`: gateway down for 2 minutes;
- `OpenClawHighMemory`: RSS above 2 GiB for 10 minutes;
- `OpenClawHighLLMErrorRate`: error ratio over 10% for 10 minutes;
- `OpenClawToolErrorSpike`: over five errors in five minutes;
- `OpenClawMonitorDroppingEvents`: any drops in ten minutes.

These are initial examples, not universal safe thresholds. Operators must tune
them for model latency, host memory, workload size, and expected tool failures.
