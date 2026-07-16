import { writable } from 'svelte/store';

export const adminKey = writable('');
export const metadataFields = writable({});
export const toasts = writable([]);

let toastId = 0;

export function addToast(message, type = 'error', duration = 4000) {
  const id = ++toastId;
  toasts.update(t => [...t, { id, message, type }]);
  setTimeout(() => {
    toasts.update(t => t.filter(x => x.id !== id));
  }, duration);
}
