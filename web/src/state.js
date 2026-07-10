const ranges = {
  "1h": { ms: 3600000, bucket: "1m" },
  "6h": { ms: 21600000, bucket: "5m" },
  "24h": { ms: 86400000, bucket: "5m" },
  "7d": { ms: 604800000, bucket: "1h" },
  "30d": { ms: 2592000000, bucket: "1d" },
};

export function timeFilters(range = "24h", instanceId = "", agentId = "") {
  const now = new Date();
  const selected = ranges[range] || ranges["24h"];
  return { range, from: new Date(now.getTime() - selected.ms).toISOString(), to: now.toISOString(), bucket: selected.bucket, instanceId, agentId };
}

export const RANGE_KEYS = Object.keys(ranges);
