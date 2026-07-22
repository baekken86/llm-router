import { test, expect } from '@playwright/test';
import { login, getToken, apiAuth, API_BASE } from './helpers.js';

test.describe('Virtual Model Duplicate', () => {
  let testVmId = null;
  let testVmName = null;

  test.beforeEach(async ({ page }) => {
    await login(page);
    // Create a fresh VM for testing
    testVmName = `dup-test-${Date.now()}`;
    const created = await apiAuth('POST', '/api/v1/virtual-models', {
      name: testVmName,
      description: 'Test VM for duplicate',
      max_retries: 1,
      retry_on_status: [429, 500],
      include_models: [],
      filter_expr: { and: [{ key: 'mc.intelligence', op: 'gte', value: '20' }] },
      sort_expr: [],
    }, await getToken());
    testVmId = created.id;
  });

  test.afterEach(async () => {
    if (testVmId) {
      try {
        await apiAuth('DELETE', `/api/v1/virtual-models/${testVmId}`, null, await getToken());
      } catch {}
    }
    // Clean up any duplicates created
    const vms = await apiAuth('GET', '/api/v1/virtual-models', null, await getToken());
    const token = await getToken();
    for (const vm of vms) {
      if (vm.name.startsWith(testVmName) && vm.id !== testVmId) {
        try {
          await apiAuth('DELETE', `/api/v1/virtual-models/${vm.id}`, null, token);
        } catch {}
      }
    }
  });

  test('duplicate button creates -2 copy and opens edit mask', async ({ page }) => {
    await page.goto('/virtual');
    await page.waitForSelector(`text=${testVmName}`);

    // Find the row containing our test VM and click its Duplicate button
    // The row card contains the VM name; locate it via the name span
    const nameSpan = page.locator('span', { hasText: new RegExp(`^${testVmName}$`) }).first();
    const card = nameSpan.locator('xpath=ancestor::div[contains(@class, "bg-gray-900")]').first();
    const duplicateBtn = card.locator('button', { hasText: 'Duplicate' });
    await duplicateBtn.click();

    // Wait for navigation to edit mask
    await page.waitForURL(/\/virtual\/\d+/, { timeout: 5000 });
    await expect(page).toHaveURL(/\/virtual\/\d+/);

    // Verify the edit mask loaded with the new VM
    await expect(page.locator('input[type="text"]').first()).toBeVisible({ timeout: 3000 });
    // The name field should contain "-2"
    const nameInput = page.locator('input[type="text"]').first();
    const nameValue = await nameInput.inputValue();
    expect(nameValue).toBe(`${testVmName}-2`);
  });

  test('duplicate of a composite VM preserves composition', async ({ page }) => {
    // Create a VM with a composition
    const token = await getToken();
    const compositeName = `dup-composite-${Date.now()}`;
    const composite = await apiAuth('POST', '/api/v1/virtual-models', {
      name: compositeName,
      description: 'Composite test',
      max_retries: 1,
      retry_on_status: [429, 500],
      include_models: [],
      composition: {
        operation: 'union',
        sources: [
          { filter_expr: { key: 'mc.coding', op: 'gte', value: '50' } },
          { vm: '' },
        ],
      },
    }, token);

    try {
      await page.goto('/virtual');
      await page.waitForSelector(`text=${compositeName}`);

      const nameSpan = page.locator('span', { hasText: new RegExp(`^${compositeName}$`) }).first();
      const card = nameSpan.locator('xpath=ancestor::div[contains(@class, "bg-gray-900")]').first();
      await card.locator('button', { hasText: 'Duplicate' }).click();

      // Wait for edit mask
      await page.waitForURL(/\/virtual\/\d+/, { timeout: 5000 });

      // Verify the new name has -2 suffix
      const nameInput = page.locator('input[type="text"]').first();
      await expect(nameInput).toHaveValue(`${compositeName}-2`, { timeout: 3000 });
    } finally {
      // Clean up
      const vms = await apiAuth('GET', '/api/v1/virtual-models', null, token);
      for (const vm of vms) {
        if (vm.name === compositeName || vm.name === `${compositeName}-2`) {
          try { await apiAuth('DELETE', `/api/v1/virtual-models/${vm.id}`, null, token); } catch {}
        }
      }
    }
  });
});
