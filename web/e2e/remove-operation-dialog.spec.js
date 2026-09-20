import { test, expect } from '@playwright/test';
import { createServer } from 'vite';

let server;
let url;
test.beforeAll(async () => {
  server = await createServer({ server: { port: 0 } });
  await server.listen();
  url = server.resolvedUrls.local[0];
});
test.afterAll(async () => { await server?.close(); });

async function mount(page, component, props) {
  await page.goto(url);
  await page.evaluate(async ({ component, props }) => {
    const { mount } = await import('/node_modules/svelte/src/index-client.js');
    const Component = (await import(`/src/components/${component}.svelte`)).default;
    document.body.innerHTML = '<form id="fixture"></form>';
    window.changes = [];
    window.submits = 0;
    const form = document.querySelector('#fixture');
    form.addEventListener('submit', e => { e.preventDefault(); window.submits++; });
    mount(Component, { target: form, props: { ...props,
      onChange: value => window.changes.push(value),
      onFlatten: value => window.changes.push(value),
    } });
  }, { component, props });
}

for (const choice of ['Cancel', 'Remove with children', 'Flatten children']) {
  test(`composition root: ${choice} without submitting`, async ({ page }) => {
    await mount(page, 'CompositionCanvas', { allVMs: [{ name: 'example' }], node: {
      __id: 'root', operation: 'difference', _expanded: true,
      sources: [{ __id: 'a', vm: 'a' }, { __id: 'b', vm: 'b' }],
    } });
    await page.getByTitle('Remove operation', { exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('multiple sources are kept under a new Union');
    await dialog.getByRole('button', { name: choice, exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const operations = page.getByTitle('Remove operation', { exact: true });
    if (choice === 'Remove with children') {
      await expect(operations).toHaveCount(0);
      await expect(page.getByTitle('Remove source', { exact: true })).toHaveCount(0);
    } else {
      await expect(operations).toHaveCount(1);
      await expect(page.locator('select').first()).toHaveValue(choice === 'Cancel' ? 'difference' : 'union');
      await expect(page.getByTitle('Remove source', { exact: true })).toHaveCount(2);
    }
    expect(await page.evaluate(() => window.submits)).toBe(0);
  });
}

for (const choice of ['Cancel', 'Remove with children', 'Flatten children']) {
  test(`filter group: ${choice} without submitting`, async ({ page }) => {
    await mount(page, 'ConditionBuilder', { node: { and: [{ __id: 'group', and: [{ __id: 'a', key: 'm.name', op: 'eq', value: 'a' }] }] } });
    await page.getByTitle('Remove group', { exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(dialog.getByRole('button', { name: 'Remove with children' })).toBeFocused();
    await dialog.getByRole('button', { name: choice, exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const changes = await page.evaluate(() => window.changes);
    if (choice === 'Cancel') expect(changes).toEqual([]);
    else if (choice === 'Remove with children') expect(changes[0].and).toEqual([]);
    else expect(changes[0].and[0].__id).toBe('a');
    expect(await page.evaluate(() => window.submits)).toBe(0);
  });
}

test('Escape and backdrop cancel restore focus without changes', async ({ page }) => {
  await mount(page, 'ConditionBuilder', { node: { and: [{ and: [{ key: 'm.name', op: 'eq', value: 'a' }] }] } });
  const trigger = page.getByTitle('Remove group', { exact: true });
  await trigger.click();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await page.getByRole('button', { name: 'Cancel removal', exact: true }).click({ position: { x: 2, y: 2 } });
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.changes)).toEqual([]);
});
