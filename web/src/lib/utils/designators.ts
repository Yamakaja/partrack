const rangeRe = /^([A-Za-z_]+)(\d+)-[A-Za-z_]*(\d+)$/;

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
