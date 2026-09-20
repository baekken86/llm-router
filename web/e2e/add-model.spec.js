import { test, expect } from '@playwright/test';
import { login, getToken, apiAuth } from './helpers.js';
import { execSync } from 'child_process';

// Verifies the manual "Add Model" flow sends string metadata values
// (regression: numbers in the payload caused `invalid request body` from the backend).
test('add model entry with numeric metadata succeeds', async ({ page }) => {
  test.setTimeout(20_000);
  await login(page);
  await page.goto('/models');
  await page.reload();

  // Unique name so the test is repeatable
  const modelName = 'test/add-model-e2e';

  await page.getByRole('button', { name: 'Add Model' }).click();
  await page.getByPlaceholder('e.g. gpt-4o').fill(modelName);

  // Labels aren't programmatically associated with inputs; locate the
  // input that follows each mc.* label in the modal's field list.
  const fieldBox = page.locator('div.space-y-4');
  for (const [key, value] of [
    ['mc.intelligence', '22'],
    ['mc.coding', '50'],
    ['mc.hallucination', '35'],
  ]) {
    await fieldBox.locator('div', { has: page.getByText(key, { exact: true }) }).locator('input').fill(value);
  }

  await page.getByRole('button', { name: 'Add Entry' }).click();

  // Success toast shown, no error toast
  await expect(page.getByText('Model entry created')).toBeVisible();
  await expect(page.getByText('invalid request body')).toHaveCount(0);

  // Verify persisted values are the string forms
  const token = await getToken();
  const entries = await apiAuth('GET', '/api/v1/model-metadata', null, token);
  const entry = entries.find((e) => e.model_name === modelName && e.reasoning_effort === '');
  expect(entry).toBeDefined();
  expect(entry.metadata.intelligence).toBe('22');
  expect(entry.metadata.coding).toBe('50');
  expect(entry.metadata.hallucination).toBe('35');

  // Cleanup via direct sqlite (no API endpoint for deleting metadata entries)
  execSync(
    `sqlite3 ~/.local/share/llm-router/llm-router.db "DELETE FROM model_metadata_global WHERE model_name = '${modelName}'"`
  );
});
