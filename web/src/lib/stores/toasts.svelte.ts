export type ToastType = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  id: string;
  type: ToastType;
  message: string;
  detail?: string;
}

export const toasts = $state<Toast[]>([]);

let _counter = 0;

export function pushToast(type: ToastType, message: string, detail?: string) {
  const id = String(++_counter);
  toasts.push({ id, type, message, detail });
  setTimeout(() => dismissToast(id), 5000);
  return id;
}

export function dismissToast(id: string) {
  const idx = toasts.findIndex(t => t.id === id);
  if (idx !== -1) toasts.splice(idx, 1);
}
