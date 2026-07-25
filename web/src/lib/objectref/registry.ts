import { Cpu, FolderKanban, GitBranch, ShoppingCart, Hammer, Box } from 'lucide-svelte';
import type { ObjectKind } from '$lib/types/objectRef.js';

// How each object kind is *presented*. The kind union and wire contract live in
// $lib/types/objectRef; this module adds icon, styling, status vocabulary, and
// the client-side mirror of internal/objectref's label rules.

type LucideIcon = typeof Cpu;

export interface KindMeta {
  /** Human name of the type itself, e.g. for an empty-state or a chip tooltip. */
  label: string;
  icon: LucideIcon;
  /** Identity is a code (an MPN, an order number) rather than a written name. */
  mono: boolean;
}

export const KIND_META: Record<ObjectKind, KindMeta> = {
  part: { label: 'Part', icon: Cpu, mono: true },
  project: { label: 'Project', icon: FolderKanban, mono: false },
  revision: { label: 'BOM revision', icon: GitBranch, mono: false },
  order: { label: 'Order', icon: ShoppingCart, mono: true },
  build: { label: 'Build', icon: Hammer, mono: false },
  storage: { label: 'Storage location', icon: Box, mono: false },
};

export function kindIcon(kind: string): LucideIcon {
  return KIND_META[kind as ObjectKind]?.icon ?? KIND_META.part.icon;
}

// ── Status vocabulary ─────────────────────────────────────────────────────
//
// One table for every kind's statuses, replacing the statusVariant/statusLabel
// pairs that were copy-pasted across the order and build screens (and had
// already drifted). `tone` maps onto the semantic palette in app.css, so
// meaning stays fixed wherever a status is drawn.

export type Tone = 'success' | 'warning' | 'attention' | 'danger' | 'reserved' | 'info' | 'muted';

export interface StatusMeta {
  label: string;
  tone: Tone;
  /**
   * Whether an inline reference carries a coloured dot for this status. Reserved
   * for states a reader should notice mid-sentence — a healthy or terminal state
   * stays quiet so the dot keeps meaning something.
   */
  dot: boolean;
}

const STATUSES: Partial<Record<ObjectKind, Record<string, StatusMeta>>> = {
  part: {
    active: { label: 'Active', tone: 'success', dot: false },
    nrnd: { label: 'NRND', tone: 'warning', dot: true },
    deprecated: { label: 'Deprecated', tone: 'muted', dot: true },
    obsolete: { label: 'Obsolete', tone: 'danger', dot: true },
    no_longer_available: { label: 'No longer available', tone: 'danger', dot: true },
  },
  project: {
    active: { label: 'Active', tone: 'success', dot: false },
    archived: { label: 'Archived', tone: 'muted', dot: true },
    completed: { label: 'Completed', tone: 'success', dot: false },
  },
  order: {
    draft: { label: 'Draft', tone: 'muted', dot: false },
    exported: { label: 'Exported', tone: 'info', dot: false },
    submitted: { label: 'Submitted', tone: 'info', dot: false },
    placed: { label: 'Placed', tone: 'info', dot: false },
    partially_received: { label: 'Partially received', tone: 'warning', dot: true },
    received: { label: 'Received', tone: 'success', dot: false },
    cancelled: { label: 'Cancelled', tone: 'muted', dot: true },
  },
  build: {
    draft: { label: 'Draft', tone: 'muted', dot: false },
    pending: { label: 'Pending', tone: 'info', dot: false },
    ordered: { label: 'Ordered', tone: 'warning', dot: true },
    ready: { label: 'Ready', tone: 'success', dot: false },
    assembled: { label: 'Assembled', tone: 'muted', dot: false },
    cancelled: { label: 'Cancelled', tone: 'danger', dot: true },
  },
};

export function statusMeta(kind: ObjectKind | string, status: string | null | undefined): StatusMeta | null {
  if (!status) return null;
  return STATUSES[kind as ObjectKind]?.[status] ?? null;
}

export interface StatusBadge {
  label: string;
  variant: string;
}

/**
 * Badge-ready status presentation. Falls back to the raw status on an unknown
 * value so a new server-side status shows up rather than vanishing.
 */
export function statusBadge(kind: ObjectKind | string, status: string | null | undefined): StatusBadge | null {
  if (!status) return null;
  const meta = statusMeta(kind, status);
  if (!meta) return { label: status.replace(/_/g, ' '), variant: 'secondary' };
  return { label: meta.label, variant: TONE_BADGE[meta.tone] };
}

/** Tailwind background class for a tone, for dots and bars. */
export const TONE_BG: Record<Tone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  attention: 'bg-attention',
  danger: 'bg-danger',
  reserved: 'bg-reserved',
  info: 'bg-info',
  muted: 'bg-muted-foreground',
};

/** Badge variant for a tone — Badge already carries the semantic variants. */
export const TONE_BADGE: Record<Tone, string> = {
  success: 'success',
  warning: 'warning',
  attention: 'attention',
  danger: 'danger',
  reserved: 'reserved',
  info: 'info',
  muted: 'secondary',
};

// ── Routing ───────────────────────────────────────────────────────────────

/**
 * Frontend route for an object, or null when it can't be derived here. A
 * revision lives under its project, so its href only comes from the server
 * (or an explicit prop) — the id alone is not enough.
 */
export function objectHref(kind: ObjectKind, teamId: string, id: string): string | null {
  if (!teamId || !id) return null;
  const base = `/teams/${teamId}`;
  switch (kind) {
    case 'part':
      return `${base}/parts/${id}`;
    case 'project':
      return `${base}/projects/${id}`;
    case 'order':
      return `${base}/orders/${id}`;
    case 'build':
      return `${base}/builds/${id}`;
    case 'storage':
      return `${base}/storage/${id}`;
    default:
      return null;
  }
}

// ── Labels ────────────────────────────────────────────────────────────────
//
// Mirrors internal/objectref/label.go. The server labels everything it can, but
// a card that has already fetched the full object formats locally rather than
// making a second request for a string it is holding the parts of. Keep the two
// in step — they are the same rules, expressed twice on purpose.

/**
 * Identify an order by distributor plus the most specific reference the team has
 * given it, falling back to the short id the order page titles itself with.
 */
export function orderLabel(
  distributor: string,
  purchaseOrderNumber: string | null | undefined,
  externalReference: string | null | undefined,
  orderId: string,
): string {
  const d = (distributor ?? '').trim();
  const ref = (purchaseOrderNumber ?? '').trim() || (externalReference ?? '').trim();
  if (ref) return `${d} #${ref.replace(/^#/, '')}`.trim();
  const short = orderId.split('-')[0] ?? '';
  return short ? `${d} ${short}`.trim() : d;
}

/**
 * Identify a BOM revision by project plus revision. A build is identified the
 * same way, since a build assembles exactly one revision.
 */
export function revisionLabel(projectName: string, revision: string): string {
  const p = (projectName ?? '').trim();
  let r = (revision ?? '').trim();
  if (!r) return p;
  const lower = r.toLowerCase();
  if (!lower.startsWith('rev') && !lower.startsWith('v')) r = `rev ${r}`;
  return p ? `${p} ${r}` : r;
}
