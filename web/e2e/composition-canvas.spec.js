import { test, expect } from '@playwright/test';
import { login } from './helpers.js';

test.describe('Composition Canvas', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await page.goto('/virtual/create');
    await page.waitForSelector('text=Composition');
  });

  test('root-level + Source button adds a filter source', async ({ page }) => {
    const addSourceBtn = page.locator('button').filter({ hasText: '+ Source' }).last();
    await addSourceBtn.click();

    // Source card has a "src" span and a select for model selection
    const srcSpan = page.locator('span').filter({ hasText: /^src$/ }).first();
    await expect(srcSpan).toBeVisible({ timeout: 3000 });
  });

  test('root-level + Op button adds an operation', async ({ page }) => {
    const addOpBtn = page.locator('button').filter({ hasText: '+ Op' }).last();
    await addOpBtn.click();

    // Operation renders a select with union/intersection/difference
    const opSelect = page.locator('select').first();
    await expect(opSelect).toBeVisible({ timeout: 3000 });
    await expect(opSelect).toHaveValue('union');
  });

  test('clicking + Source on operation adds a child', async ({ page }) => {
    // Add an operation at root (starts expanded with _expanded: true)
    const addOpBtn = page.locator('button').filter({ hasText: '+ Op' }).last();
    await addOpBtn.click();

    // Verify operation shows "0 sources"
    await expect(page.locator('text=0 sources')).toBeVisible({ timeout: 3000 });

    // The operation starts expanded, so + Source buttons inside it are visible.
    // Click the first + Source (inside the op), not the root-level one.
    const addSourceBtn = page.locator('button').filter({ hasText: '+ Source' }).first();
    await addSourceBtn.click();

    // Source count should update to "1 sources"
    await expect(page.locator('text=1 sources')).toBeVisible({ timeout: 3000 });
  });

  test('root-level + Source wraps existing content in union', async ({ page }) => {
    // First add a source at root
    let addSourceBtn = page.locator('button').filter({ hasText: '+ Source' }).last();
    await addSourceBtn.click();

    // Now add another source — should wrap in union
    addSourceBtn = page.locator('button').filter({ hasText: '+ Source' }).last();
    await addSourceBtn.click();

    // Should now have a union operation as root
    const opSelect = page.locator('select').first();
    await expect(opSelect).toBeVisible({ timeout: 3000 });
    await expect(opSelect).toHaveValue('union');
  });
});
