import { test as base, expect } from '@playwright/test';

const API_BASE = process.env.BASE_URL || 'http://localhost:19922';
const ADMIN_PASSWORD = process.env.LLM_ROUTER_ADMIN_PASSWORD || 'mein-geheimnis';

/**
 * Authenticate via API and store token in localStorage.
 * Must navigate to the app first so localStorage is accessible.
 */
export async function login(page) {
  await page.goto('/');
  const res = await page.request.post(`${API_BASE}/api/v1/admin/auth`, {
    data: { password: ADMIN_PASSWORD },
  });
  const body = await res.json();
  await page.evaluate((token) => {
    localStorage.setItem('adminToken', token);
  }, body.token);
}

/**
 * Navigate to a path and reload to trigger auth check with stored token.
 */
export async function navigateTo(page, path) {
  await page.goto(path);
  // Reload so the SPA picks up the stored token
  await page.reload();
}

/**
 * Call the backend API directly (bypasses browser).
 */
export async function apiCall(method, path, data = null) {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: data ? JSON.stringify(data) : undefined,
  });
  if (!res.ok) throw new Error(`${method} ${path} failed: ${res.status} ${await res.text()}`);
  if (res.status === 204) return null;
  return res.json();
}

/**
 * Authenticate and return a token for direct API calls.
 */
export async function getToken() {
  const res = await fetch(`${API_BASE}/api/v1/admin/auth`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: ADMIN_PASSWORD }),
  });
  const body = await res.json();
  return body.token;
}

/**
 * Authenticated API call using a token.
 */
export async function apiAuth(method, path, data = null, token) {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    body: data ? JSON.stringify(data) : undefined,
  });
  if (!res.ok) throw new Error(`${method} ${path} failed: ${res.status} ${await res.text()}`);
  if (res.status === 204) return null;
  return res.json();
}

export { API_BASE, ADMIN_PASSWORD };
