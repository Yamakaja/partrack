/**
 * Formats a decimal string (e.g. "10.4500") as a currency string.
 * @param decimals - fixed decimal places; defaults to 2
 */
export function formatPrice(
  value: string | null | undefined,
  currency = 'EUR',
  decimals = 2,
): string {
  if (!value) return '—';
  const n = parseFloat(value);
  if (isNaN(n)) return '—';
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency,
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(n);
}

/** Returns the raw decimal string as a display-friendly string (no currency symbol). */
export function formatDecimal(value: string | null | undefined): string {
  if (!value) return '—';
  const num = Number(value);
  if (isNaN(num)) return value;
  return num.toFixed(4).replace(/\.?0+$/, '');
}
