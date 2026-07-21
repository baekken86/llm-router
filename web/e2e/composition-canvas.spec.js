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
    // Add an operation at root
    const addOpBtn = page.locator('button').filter({ hasText: '+ Op' }).last();
    await addOpBtn.click();

    // Click + Source on the operation (first one on page, inside the op)
    const addSourceBtn = page.locator('button').filter({ hasText: '+ Source' }).first();
    await addSourceBtn.click();

    // A second select should appear (the source card's model dropdown)
    const selects = page.locator('select');
    await expect(selects).toHaveCount(2, { timeout: 3000 });
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
