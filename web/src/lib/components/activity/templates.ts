// Declarative rendering for activity-log entries. The backend emits structured,
// self-contained events; this table turns an `action` + payload into a sentence
// with clickable entity links. Adding an event type = one template line here.
//
// An action with no template falls back to the backend-provided `summary`, so a
// new backend event never breaks an older client.

import type { ActivityEntry } from '$lib/api/activityLog.js';

// action → sentence template. Tokens: {subject}, {refs.X}, {values.X}.
export const templates: Record<string, string> = {
  // Scanner (personal feed)
  'scan.bag_scanned': 'Scanned {subject}',
  'scan.part_imported': 'Imported {subject}',
  'scan.order_received': 'Received {values.qty}× {subject}',
  // Team feed
  'part.created': 'Created part {subject}',
  'project.created': 'Created project {subject}',
  'bom.imported': 'Imported BOM {subject}: {values.total} lines, {values.resolved} resolved',
  'build.submitted': 'Submitted {subject} ({values.quantity}× boards)',
  'build.assembled': 'Assembled {subject} ({values.parts_deducted} part(s) deducted)',
  'build.cancelled': 'Cancelled {subject}',
  'order.item_received': 'Received {values.qty} unit(s) on {subject}',
  'order.received': 'Received {subject} in full ({values.lines} line(s))',
  'order.cancelled': 'Cancelled {subject}',
  'order.external_linked': 'Linked {subject} to a distributor order',
  'inventory.adjusted': 'Adjusted stock ({values.delta}) on {subject}',
};

const icons: Record<string, string> = {
  'scan.order_received': '📦',
  'scan.stock_adjusted': '±',
  'scan.storage_assigned': '📍',
  'scan.part_imported': '✦',
  'scan.part_not_found': '?',
  'scan.bag_scanned': '⬡',
  'scan.kitting_confirmed': '✓',
  'scan.kitting_shortfall': '⚠',
  'part.created': '✦',
  'project.created': '🗂',
  'bom.imported': '📋',
  'build.submitted': '🔧',
  'build.assembled': '🔩',
  'build.cancelled': '✕',
  'checkout.created': '➖',
  'cart.converted': '🛒',
  'order.item_received': '📦',
  'order.received': '📦',
  'order.cancelled': '✕',
  'order.external_linked': '🔗',
  'inventory.adjusted': '±',
};

// Fallback label for a subject that has no snapshot label (e.g. a build/order
// identified only by id), so the inline link still reads naturally.
const typeLabel: Record<string, string> = {
  part: 'the part',
  order: 'the order',
  build: 'the build',
  project: 'the project',
  revision: 'the revision',
  storage: 'the location',
};

export function actionIcon(action: string): string {
  return icons[action] ?? '·';
}

interface RefShape {
  type: string;
  id: string;
  label: string;
  parent?: { type: string; id: string };
}

// Maps an entity ref to its detail route, or null when the type has no page
// (e.g. user) — in which case it renders as bold text rather than a link.
export function routeForRef(ref: { type: string; id: string; parent?: { type: string; id: string } }, teamId: string): string | null {
  switch (ref.type) {
    case 'part': return `/teams/${teamId}/parts/${ref.id}`;
    case 'order': return `/teams/${teamId}/orders/${ref.id}`;
    case 'storage': return `/teams/${teamId}/storage/${ref.id}`;
    case 'build': return `/teams/${teamId}/builds/${ref.id}`;
    case 'project': return `/teams/${teamId}/projects/${ref.id}`;
    case 'revision': return ref.parent ? `/teams/${teamId}/projects/${ref.parent.id}/revisions/${ref.id}` : null;
    default: return null;
  }
}

export type Segment =
  | { kind: 'text'; text: string }
  // refType/id are carried so the row can render a real ObjectRef (link + hover
  // card) rather than a bare anchor. url stays because a revision's route can't
  // be derived from its id alone.
  | { kind: 'ref'; refType: string; id: string; label: string; url: string | null };

// Tokenizes an entry's template and resolves each slot against its payload.
export function renderSegments(item: ActivityEntry, teamId: string): Segment[] {
  const tmpl = templates[item.action];
  if (!tmpl) return [{ kind: 'text', text: item.summary ?? item.action }];

  const payload = (item.payload ?? {}) as {
    refs?: Record<string, RefShape>;
    values?: Record<string, unknown>;
  };

  const segments: Segment[] = [];
  const re = /\{([^}]+)\}/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(tmpl)) !== null) {
    if (m.index > last) segments.push({ kind: 'text', text: tmpl.slice(last, m.index) });
    const seg = resolveSlot(m[1], item, payload, teamId);
    if (seg) segments.push(seg);
    last = re.lastIndex;
  }
  if (last < tmpl.length) segments.push({ kind: 'text', text: tmpl.slice(last) });
  return segments;
}

function resolveSlot(
  name: string,
  item: ActivityEntry,
  payload: { refs?: Record<string, RefShape>; values?: Record<string, unknown> },
  teamId: string,
): Segment | null {
  if (name === 'subject') {
    if (!item.subject) return null;
    const label = item.subject.label || typeLabel[item.subject.type] || 'item';
    return {
      kind: 'ref',
      refType: item.subject.type,
      id: item.subject.id,
      label,
      url: routeForRef(item.subject, teamId),
    };
  }
  if (name.startsWith('refs.')) {
    const r = payload.refs?.[name.slice(5)];
    if (!r) return null;
    return {
      kind: 'ref',
      refType: r.type,
      id: r.id,
      label: r.label || typeLabel[r.type] || 'item',
      url: routeForRef(r, teamId),
    };
  }
  if (name.startsWith('values.')) {
    const v = payload.values?.[name.slice(7)];
    return { kind: 'text', text: v == null ? '' : String(v) };
  }
  return null;
}
