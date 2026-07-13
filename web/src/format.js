export const esc = (value) => String(value ?? "").replace(/[&<>'"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[c]);
export const num = (value, digits = 0) => new Intl.NumberFormat("zh-CN", { maximumFractionDigits: digits }).format(Number(value || 0));
export const compact = (value) => new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 }).format(Number(value || 0));
export const money = (value) => `$${Number(value || 0).toFixed(Number(value || 0) < 1 ? 4 : 2)}`;
export const ms = (value) => Number(value || 0) >= 1000 ? `${(Number(value) / 1000).toFixed(1)}s` : `${num(value)}ms`;
export const bytes = (value) => {
  const amount = Number(value || 0);
  if (amount >= 1099511627776) return `${(amount / 1099511627776).toFixed(1)} TiB`;
  if (amount >= 1073741824) return `${(amount / 1073741824).toFixed(1)} GiB`;
  if (amount >= 1048576) return `${(amount / 1048576).toFixed(1)} MiB`;
  return `${num(amount)} B`;
};
const _hourFmt = new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" });
const _fullFmt = new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

// Compact time label for chart axes and table cells.
// When rangeMs is provided and ≤ 24h, only show HH:MM (no date prefix).
let _currentRangeMs = Infinity;
export function setShortTimeRange(rangeMs) { _currentRangeMs = rangeMs || Infinity; }
export const shortTime = (value) => {
  const d = new Date(value);
  return _currentRangeMs <= 86400000 ? _hourFmt.format(d) : _fullFmt.format(d);
};
export const fullShortTime = (value) => _fullFmt.format(new Date(value));
