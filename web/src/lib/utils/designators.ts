const rangeRe = /^([A-Za-z_]+)(\d+)-[A-Za-z_]*(\d+)$/;
const splitRe = /^([A-Za-z_]*)(\d*)/;

/**
 * Order two reference-designator strings the way a schematic reads: by prefix,
 * then numerically, so R9 sorts before R10 rather than after it. Compares on each
 * string's first designator, which is the one a grouped BOM line is named by.
 */
export function compareDesignators(a: string, b: string): number {
  const first = (s: string) => (s.split(',')[0] ?? '').trim();
  const [, ap = '', an = ''] = first(a).match(splitRe) ?? [];
  const [, bp = '', bn = ''] = first(b).match(splitRe) ?? [];
  if (ap !== bp) return ap.localeCompare(bp);
  if (an && bn) return Number(an) - Number(bn);
  return first(a).localeCompare(first(b));
}

/** Expand a reference designator string like "R1, R3, R5-R8, C1" into individual designators. */
export function expandDesignators(s: string): string[] {
	const result: string[] = [];
	for (const part of s.split(',')) {
		const tok = part.trim();
		if (!tok) continue;
		const m = tok.match(rangeRe);
		if (m) {
			const prefix = m[1];
			let lo = parseInt(m[2], 10);
			let hi = parseInt(m[3], 10);
			if (lo > hi) [lo, hi] = [hi, lo];
			for (let n = lo; n <= hi; n++) result.push(`${prefix}${n}`);
		} else {
			result.push(tok);
		}
	}
	return result;
}
