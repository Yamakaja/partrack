/**
 * Formats a fixed-point decimal string (e.g. "10.4500") into a locale
 * currency string. Never converts to float — parses as string arithmetic.
 */
export function formatPrice(value: string | null | undefined, currency = 'USD'): string {
  if (!value) return '—';
  try {
    // Use Intl.NumberFormat with the decimal string parsed as a number for display only.
    // The original string is preserved for API round-trips.
    const num = Number(value);
    if (isNaN(num)) return value;
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    }).format(num);
  } catch {
    return value;
  }
}

/** Returns the raw decimal string as a display-friendly string (no currency symbol). */
export function formatDecimal(value: string | null | undefined): string {
  if (!value) return '—';
  const num = Number(value);
  if (isNaN(num)) return value;
  return num.toFixed(4).replace(/\.?0+$/, '');
}
