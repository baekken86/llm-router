import { get } from 'svelte/store';
import { adminKey } from './stores.js';

export async function apiFetch(url, options = {}) {
  const key = get(adminKey);
  const headers = { ...options.headers };
  if (key) {
    headers['Authorization'] = `Bearer ${key}`;
  }
  if (options.body && typeof options.body === 'object' && !(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(options.body);
  }
  const res = await fetch(url, { ...options, headers });
  if (!res.ok) {
    const text = await res.text();
    let msg;
    try { msg = JSON.parse(text).error; } catch { msg = text; }
    throw new Error(msg || `HTTP ${res.status}`);
  }
  if (res.status === 204) return null;
  return res.json();
}

export function getOperatorsForType(type) {
  switch (type) {
    case 'number':
      return [
        { op: 'eq', label: '=', description: 'equals' },
        { op: 'neq', label: '≠', description: 'not equals' },
        { op: 'gt', label: '>', description: 'greater than' },
        { op: 'gte', label: '≥', description: 'greater or equal' },
        { op: 'lt', label: '<', description: 'less than' },
        { op: 'lte', label: '≤', description: 'less or equal' }
      ];
    case 'string':
      return [
        { op: 'eq', label: '=', description: 'equals' },
        { op: 'neq', label: '≠', description: 'not equals' },
        { op: 'in', label: 'in', description: 'is one of' },
        { op: 'contains', label: 'contains', description: 'contains text' }
      ];
    case 'boolean':
      return [
        { op: 'eq', label: '=', description: 'equals' },
        { op: 'neq', label: '≠', description: 'not equals' }
      ];
    default:
      return [
        { op: 'eq', label: '=', description: 'equals' },
        { op: 'neq', label: '≠', description: 'not equals' }
      ];
  }
}
