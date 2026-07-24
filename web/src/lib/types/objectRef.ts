// Wire types for partrack's linkable objects. Mirrors internal/objectref —
// these strings are a contract shared by note markdown directives, activity-log
// subject types, and the object-refs endpoint's query parameters.

export type ObjectKind = 'part' | 'project' | 'revision' | 'order' | 'build' | 'storage';

export const OBJECT_KINDS: ObjectKind[] = ['part', 'project', 'revision', 'order', 'build', 'storage'];

/**
 * A render-ready reference to one object: enough to draw an inline ref or a
 * table cell without knowing the object's type.
 *
 * `label` is the identity — what makes the object recognisable and what reads
 * inside a sentence ("Cancelled Blinky rev B"). `qualifier` is the short detail
 * a dense row adds beside it to tell two similar objects apart ("×10"); prose
 * omits it.
 */
export interface ObjectSummary {
  kind: ObjectKind;
  id: string;
  label: string;
  qualifier?: string;
  status?: string;
  href?: string;
}
