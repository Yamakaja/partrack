import { formatSI } from './si.js';
import type { BOMInterpretation, BOMLineItem } from '$lib/types/projects.js';
import type { ParameterDefinition } from '$lib/types/parts.js';

/** The package constraint is carried as an ordinary param under this slug. */
export const PACKAGE_SLUG = 'package';

export type ParamDefLookup = (slug: string) => ParameterDefinition | undefined;

/**
 * One rendered parameter constraint.
 *
 * A pill must be readable on its own. Parameters that carry a unit are
 * self-describing once formatted ("316 kΩ", "2.54 mm"); parameters that don't
 * are bare figures and need their name attached, or a connector line renders as
 * a meaningless "4 1 0.00254". `label` is set only in that second case.
 */
export interface SpecPill {
  label: string | null;
  value: string;
}

/** Trim the boilerplate off a definition's display name for use as a pill label. */
function shortLabel(def: ParameterDefinition | undefined, slug: string): string {
  const name = def?.display_name ?? slug.replace(/_/g, ' ');
  return name.replace(/^Number of\s+/i, '');
}

/**
 * Render one interpretation param. Returns null for the package param, which the
 * caller places separately, and for params with nothing displayable.
 */
export function paramPill(
  p: BOMInterpretation['params'][number],
  lookup: ParamDefLookup,
): SpecPill | null {
  if (p.slug === PACKAGE_SLUG) return null;

  const def = lookup(p.slug);

  // Non-numeric (enum/text) params carry their meaning in the raw token.
  if (p.value_si == null) {
    return p.raw ? { label: null, value: p.raw } : null;
  }

  // Tolerance reads as a signed band rather than a bare percentage.
  if (p.slug === 'tolerance') return { label: null, value: `±${p.value_si}%` };

  const unit = def?.unit_symbol;
  if (unit) return { label: null, value: formatSI(p.value_si, unit) };

  return { label: shortLabel(def, p.slug), value: String(p.value_si) };
}

/** The raw package token, if the parser extracted one. */
export function packageOf(interp: BOMInterpretation | null | undefined): string | null {
  return interp?.params?.find((p) => p.slug === PACKAGE_SLUG)?.raw ?? null;
}

/** True once the parse produced a magnitude — a value with a unit behind it. */
function hasMagnitude(interp: BOMInterpretation | null | undefined, lookup: ParamDefLookup): boolean {
  return (interp?.params ?? []).some(
    (p) => p.slug !== PACKAGE_SLUG && p.value_si != null && !!lookup(p.slug)?.unit_symbol,
  );
}

export interface DesignedSpec {
  pills: SpecPill[];
  pkg: string | null;
  /** Whether `pills` already contains the raw value, so echoing it would duplicate. */
  echoesValue: boolean;
}

/**
 * What the design asked for, as the parser understood it.
 *
 * Two shapes, chosen by whether a magnitude came out of the parse:
 *  - magnitude present (R/C/L): the parsed constraints lead ("316 kΩ · ±1% · 0402")
 *    and the raw value stays available underneath as a cross-check.
 *  - no magnitude (LEDs, ferrite beads, connectors matched on package alone): the
 *    parse has nothing to say the value doesn't, so the raw value leads instead —
 *    otherwise the cell would show a lone "0402" and drop the descriptor entirely.
 */
export function designedSpec(item: BOMLineItem, lookup: ParamDefLookup): DesignedSpec {
  const interp = item.interpretation;
  const pills: SpecPill[] = [];
  const echoesValue = !hasMagnitude(interp, lookup);

  if (echoesValue && item.value) pills.push({ label: null, value: item.value });
  for (const p of interp?.params ?? []) {
    const pill = paramPill(p, lookup);
    if (pill) pills.push(pill);
  }
  if (!pills.length && item.value) pills.push({ label: null, value: item.value });

  return { pills, pkg: packageOf(interp), echoesValue };
}

// ── Axis A: how the request was read ────────────────────────────────────────────

export type TrustTone = 'success' | 'info' | 'warning';

export interface TrustSignal {
  tone: TrustTone;
  label: string;
  detail: string;
}

/**
 * Interpretation provenance, surfaced only when it is worth a second look.
 *
 * Silent for a deterministic parse at full confidence, which is the common case —
 * marking every row would bury the rows that actually need attention. The reported
 * confidence is deliberately not thresholded: the deterministic parser emits 1.0 or
 * a coded downgrade, while an LLM's self-reported figure is not calibrated and in
 * practice never reaches 1.0. So the signal is *whether* the LLM was involved, not
 * how sure it claims to be — the number itself belongs in the detail view.
 */
export function trustSignal(item: BOMLineItem): TrustSignal | null {
  const interp = item.interpretation;
  const flags = interp?.flags ?? [];
  const viaLLM = item.interpretation_source === 'llm' || flags.includes('llm');

  if (interp?.route === 'unknown') {
    return {
      tone: 'warning',
      label: "Couldn't classify",
      detail: 'The parser could not determine what kind of part this is.',
    };
  }
  // A human correction outranks how the value was originally read, including when
  // the LLM proposed it — confirmed is confirmed.
  if (item.interpretation_source === 'user') {
    return { tone: 'success', label: 'Confirmed', detail: 'Interpretation corrected by a person.' };
  }
  if (viaLLM) {
    return { tone: 'info', label: 'AI read', detail: 'Interpreted by the language model, not the deterministic parser.' };
  }
  if (interp && interp.confidence < 1) {
    return {
      tone: 'warning',
      label: 'Uncertain parse',
      detail: 'The parser downgraded its own confidence in this reading.',
    };
  }
  return null;
}

// ── Axis B: how certain the landing is ─────────────────────────────────────────

export type MechanismTone = 'success' | 'info' | 'warning' | 'attention' | 'danger';

interface Mechanism {
  label: string;
  tone: MechanismTone;
}

/**
 * Plain-language names for the resolution tiers, strongest first. `auto_sku` leads
 * because a distributor + SKU pair identifies exactly one listing, leaving nothing
 * to verify.
 */
const MECHANISMS: Record<string, Mechanism> = {
  auto_sku: { label: 'Listing match', tone: 'success' },
  manual: { label: 'Chosen by you', tone: 'success' },
  auto_mpn: { label: 'MPN match', tone: 'success' },
  auto_import: { label: 'Distributor import', tone: 'success' },
  auto_generic: { label: 'Parametric match', tone: 'info' },
  auto_mpn_fuzzy: { label: 'Partial MPN match', tone: 'warning' },
  ambiguous: { label: 'Needs review', tone: 'attention' },
  import_ambiguous: { label: 'Needs review', tone: 'attention' },
  import_no_result: { label: 'No match found', tone: 'danger' },
  importing: { label: 'Still importing', tone: 'info' },
  auto_prior_revision: { label: 'Matched to prior revision', tone: 'info' },
  ignored: { label: 'Ignored', tone: 'info' },
  // Retired tier, still present on revisions imported before it was replaced by the
  // resolve pipeline. Named rather than left to the fallback so those rows read.
  auto_distributor: { label: 'Distributor lookup', tone: 'success' },
};

export interface ResolutionMechanism extends Mechanism {
  /** Revision the decision was originally made in, when this row inherited it. */
  inheritedFrom: string | null;
}

/**
 * How this line arrived at its part.
 *
 * An inherited row reports the decision it descends from rather than the
 * inheritance itself: on a project that re-imports an unchanged BOM most rows are
 * inherited, so "matched to prior revision" everywhere would say nothing about why
 * any particular part was chosen.
 */
export function resolutionMechanism(item: BOMLineItem): ResolutionMechanism {
  if (item.ignored) return { label: 'Ignored', tone: 'info', inheritedFrom: null };
  if (!item.resolution_state) return { label: 'Unresolved', tone: 'danger', inheritedFrom: null };

  if (item.resolution_state === 'auto_prior_revision' && item.origin_resolution_state) {
    const origin = MECHANISMS[item.origin_resolution_state];
    return {
      label: origin?.label ?? item.origin_resolution_state,
      tone: origin?.tone ?? 'info',
      inheritedFrom: item.origin_revision_label ?? null,
    };
  }

  const m = MECHANISMS[item.resolution_state];
  // An unmapped state shows itself rather than a generic "Resolved" — better to read
  // as unfamiliar than to be quietly filed under the wrong mechanism.
  return {
    label: m?.label ?? item.resolution_state.replace(/_/g, ' '),
    tone: m?.tone ?? 'info',
    inheritedFrom: null,
  };
}

// ── stock ──────────────────────────────────────────────────────────────────────

export type StockState = 'untracked' | 'out' | 'short' | 'ok';

/** Team stock for the line, judged against one board's worth of demand. */
export function stockState(item: BOMLineItem): StockState | null {
  if (!item.resolved_part_id) return null;
  if (item.quantity_on_hand == null) return 'untracked';
  if (item.quantity_on_hand <= 0) return 'out';
  if (item.quantity_on_hand < item.quantity) return 'short';
  return 'ok';
}

/** Rows a person still has to do something about. */
export function needsAttention(item: BOMLineItem): boolean {
  if (item.ignored) return false;
  if (!item.resolved_part_id) return true;
  const stock = stockState(item);
  if (stock === 'out' || stock === 'short' || stock === 'untracked') return true;
  return item.interpretation?.route === 'unknown';
}
