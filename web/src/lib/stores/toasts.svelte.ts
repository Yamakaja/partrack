export type ToastType = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  id: string;
  type: ToastType;
  message: string;
  detail?: string;
  /** When set, render a progress bar. `total` of 0 renders an indeterminate bar. */
  progress?: { current: number; total: number };
  /** Sticky toasts never auto-dismiss; used for in-flight progress. */
  sticky?: boolean;
}

export const toasts = $state<Toast[]>([]);

const DISMISS_MS = 5000;
let _counter = 0;

export function pushToast(type: ToastType, message: string, detail?: string) {
  const id = String(++_counter);
  toasts.push({ id, type, message, detail });
  setTimeout(() => dismissToast(id), DISMISS_MS);
  return id;
}

/**
 * Push a sticky toast with a progress bar that does NOT auto-dismiss. Use for
 * long-running background work; call updateToast to advance it and settleToast
 * to convert it into a normal auto-dismissing toast on completion.
 */
export function pushProgressToast(message: string, total: number, detail?: string) {
  const id = String(++_counter);
  toasts.push({ id, type: 'info', message, detail, sticky: true, progress: { current: 0, total } });
  return id;
}

/** Patch an existing toast in place (e.g. advance its progress). No-op if dismissed. */
export function updateToast(id: string, patch: Partial<Omit<Toast, 'id'>>) {
  const t = toasts.find((t) => t.id === id);
  if (!t) return;
  Object.assign(t, patch);
}

/**
 * Convert a sticky progress toast into a normal toast and start its auto-dismiss
 * timer. No-op if the toast was already dismissed by the user.
 */
export function settleToast(id: string, type: ToastType, message: string, detail?: string) {
  const t = toasts.find((t) => t.id === id);
  if (!t) return;
  t.type = type;
  t.message = message;
  t.detail = detail;
  t.sticky = false;
  t.progress = undefined;
  setTimeout(() => dismissToast(id), DISMISS_MS);
}

export function dismissToast(id: string) {
  const idx = toasts.findIndex(t => t.id === id);
  if (idx !== -1) toasts.splice(idx, 1);
}
