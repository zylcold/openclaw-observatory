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
export const shortTime = (value) => new Date(value).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
