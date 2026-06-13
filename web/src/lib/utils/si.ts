const SI_PREFIXES: [number, string][] = [
  [1e12, 'T'], [1e9, 'G'], [1e6, 'M'], [1e3, 'k'],
  [1, ''],
  [1e-3, 'm'], [1e-6, 'µ'], [1e-9, 'n'], [1e-12, 'p'],
];

const PREFIX_MAP: Record<string, number> = {
  T: 1e12, G: 1e9, M: 1e6, k: 1e3, K: 1e3,
  '': 1,
  m: 1e-3, µ: 1e-6, u: 1e-6, n: 1e-9, p: 1e-12,
};

/**
 * Format a numeric base-SI value with an appropriate SI prefix.
 * unitSymbol must be non-null/non-empty to apply SI prefix formatting.
 * e.g. formatSI(10000, 'Ω') → '10 kΩ', formatSI(100e-9, 'F') → '100 nF'
 * Without a unit, returns the raw number as a string.
 */
export function formatSI(value: number, unitSymbol: string | null | undefined): string {
  if (!unitSymbol) return String(parseFloat(value.toPrecision(4)));
  // Compound / ratio units (e.g. "V/µs", "ppm/°C") don't compose with a single SI
  // prefix — a value is stored in the parameter's own unit, so show it as-is.
  if (unitSymbol.includes('/')) return `${parseFloat(value.toPrecision(4))} ${unitSymbol}`;
  const abs = Math.abs(value);
  for (const [factor, prefix] of SI_PREFIXES) {
    if (abs >= factor || factor === 1e-12) {
      const scaled = value / factor;
      const formatted = parseFloat(scaled.toPrecision(4)).toString();
      return `${formatted} ${prefix}${unitSymbol}`.trim();
    }
  }
  return `${value} ${unitSymbol}`.trim();
}

/**
 * Parse a string with optional SI prefix into a base SI value.
 * Only applies SI prefix parsing when unitSymbol is provided (non-empty).
 * Accepts: "100k", "4.7µ", "1 nF" (strips known unitSymbol), "1e-9", plain numbers.
 * Returns null if the input cannot be parsed.
 */
export function parseSI(input: string, unitSymbol?: string | null): number | null {
  const s = input.trim();
  if (s === '') return null;

  // Plain number / scientific notation — always accepted regardless of unit
  const plain = Number(s);
  if (!isNaN(plain)) return plain;

  // SI prefix parsing only makes sense when a unit is defined
  if (!unitSymbol) return null;

  // Strip known unit suffix (e.g. "Ω", "F", "H")
  let stripped = s;
  if (stripped.endsWith(unitSymbol)) {
    stripped = stripped.slice(0, -unitSymbol.length).trim();
  }

  // Match: number + optional whitespace + optional SI prefix
  const m = stripped.match(/^([+-]?\d*\.?\d+(?:[eE][+-]?\d+)?)\s*([TGMkmµunpK]?)$/);
  if (!m) return null;

  const num = parseFloat(m[1]);
  if (isNaN(num)) return null;
  const factor = PREFIX_MAP[m[2] ?? ''] ?? 1;
  return num * factor;
}
