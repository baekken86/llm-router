import { test, expect } from '@playwright/test';
import { flattenNodeFromComposition, flattenFilterChild, removeNodeFromComposition, assignCompositionIds } from '../src/lib/treeUtils.js';

test('composition flatten preserves ordered children, identities and nested subtrees', () => {
  const a = { __id: 'a', vm: 'a' };
  const nested = { __id: 'nested', operation: 'intersection', sources: [a] };
  const b = { __id: 'b', vm: 'b' };
  const wrapper = { __id: 'wrapper', operation: 'difference', sources: [nested, b], filter_expr: { key: 'x' } };
  const tree = { __id: 'root', operation: 'union', sources: [a, wrapper, b] };
  const result = flattenNodeFromComposition(tree, 'wrapper');
  expect(result.sources).toEqual([a, nested, b, b]);
  expect(result.sources[1]).toBe(nested);
  expect(result.sources[2]).toBe(b);
  expect(tree.sources[1]).toBe(wrapper);
  expect(removeNodeFromComposition(tree, 'wrapper').sources).toEqual([a, b]);
  expect(flattenNodeFromComposition(tree, 'missing')).toBe(tree);
  expect(flattenNodeFromComposition(tree, 'a')).toBe(tree);
});

test('composition root and empty flatten cases', () => {
  const a = { __id: 'a', vm: 'a' }, b = { __id: 'b', vm: 'b' };
  const root = { __id: 'root', operation: 'difference', sources: [] };
  expect(flattenNodeFromComposition(root, 'root')).toBeNull();
  root.sources = [a];
  expect(flattenNodeFromComposition(root, 'root')).toBe(a);
  root.sources = [a, b];
  const result = flattenNodeFromComposition(root, 'root');
  assignCompositionIds(result);
  expect(result.operation).toBe('union');
  expect(result.__id).toBeTruthy();
  expect(result.sources[0]).toBe(a);
  expect(result.sources[1]).toBe(b);
  const empty = { __id: 'empty', operation: 'union', sources: [] };
  expect(flattenNodeFromComposition({ ...root, sources: [a, empty, b] }, 'empty').sources).toEqual([a, b]);
});

for (const mode of ['and', 'or']) {
  test(`filter ${mode} splice preserves metadata, order, identities and nested groups`, () => {
    const a = { __id: 'a', key: 'a' }, b = { __id: 'b', key: 'b' };
    const nested = { __id: 'nested', and: [a] };
    const group = { __id: 'group', or: [nested, b] };
    const parent = { __id: 'parent', priority: 4, [mode]: [a, group, b] };
    const result = flattenFilterChild(parent, group);
    expect(result[mode]).toEqual([a, nested, b, b]);
    expect(result[mode][1]).toBe(nested);
    expect(result[mode][2]).toBe(b);
    expect(result.__id).toBe('parent');
    expect(result.priority).toBe(4);
    expect(parent[mode][1]).toBe(group);
    expect(flattenFilterChild(parent, { __id: 'absent', and: [] })).toBe(parent);
    expect(flattenFilterChild(parent, a)).toBe(parent);
    expect(flattenFilterChild(parent, group, [])[mode]).toEqual([a, b]);
  });
}
