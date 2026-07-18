# Unreleased

**Date:** 2026-07-18
**PR:** #2 — feat(web): hierarchical drag-and-drop for operation tree
**Merge commit:** e64693477d92edbb78bd5f37692b59130f4a91a4

## Features

### Hierarchical drag-and-drop for operation tree (#2)
Replaced the existing `moveUp`/`moveDown` arrow buttons in `SortBuilder` with drag handles, and added hierarchical drag-and-drop to the entire operation tree across both `ConditionBuilder` and `SortBuilder`.

**What works now:**
- Any node is reorderable: leaves (conditions), intermediate operations (nested AND/OR/NOT groups), top-level operations
- Cross-parent moves: drag a leaf or group into a different parent group
- Cycle prevention: cannot drop a group into one of its own descendants
- Single-item guard: drag handle hidden when only 1 item (no meaningful reorder)
- Keyboard a11y: built into `@dnd-kit/svelte` (Space to grab, arrows to move, Space to drop)
- Touch/mobile: covered via `PointerSensor` (default activation distance prevents scroll conflict)

**New components / modules:**
- `web/src/components/SortableTree.svelte` — thin `DragDropProvider` + `DragOverlay` wrapper
- `web/src/components/SortableItem.svelte` — `createSortable` wrapper with reactive getter props
- `web/src/components/DragHandle.svelte` — grip icon + `attachHandle`
- `web/src/lib/treeUtils.js` — immutable tree utilities (`moveNodeInTree`, `isDescendant`, `assignStableIds`, `findNodeAndParent`, etc.)
- `docs/adr/003-drag-drop-library-choice.md` — ADR for library selection

**Modified:**
- `web/src/components/ConditionBuilder.svelte` — wrapped tree in sortable + drag handles (replaces index-based rendering at nested levels)
- `web/src/components/SortBuilder.svelte` — removed `moveUp`/`moveDown` + arrow buttons; added sortable + drag handles

**Library:** `@dnd-kit/svelte@0.5.0` + `@dnd-kit/dom@0.5.0` (in `node_modules` from manual install, now actually wired up; `package.json` declaration fixed in PR #3).

## Bug Fixes

- Used previously-installed `@dnd-kit` packages (in `node_modules` but not declared in `package.json` — ghost dep, fixed in PR #3)

## Technical Notes

- Cycle detection: `isDescendant(tree, ancestorId, candidateId)` walks ancestor chain of dragged node. Rejects `inside` drops that would create a cycle. Double-guarded — `handleTreeDragEnd` checks before `moveNodeInTree`.
- Tree mutation: all updates immutable via spread copies. No in-place mutation.
- `createSortable` integration: uses getter pattern (`get id() { return id; }`) on `$props()` closure vars — required by library's `$effect.pre` reactive tracking. Direct object literals would snapshot values and go stale after first drag.
- 42/42 logic assertions pass on `treeUtils.js` (pure functions, no DOM required).
- All 7 test scenarios pass at code level (S1 leaf reorder, S2 group reorder, S3 reparent leaf, S4 cycle prevention, S5 SortBuilder flat reorder, S6 keyboard, S7 single-item no handle). S6 verified by library docs (KeyboardSensor default).

## Deferred (follow-up scope)

- Cursor-relative drop positioning (current heuristic: target `isGroup` → `inside`, else index compare → `before`/`after`). Acceptable for typical trees, but dense trees may want pixel-accurate drop targets.
- Playwright E2E tests (no test infra in project yet). Code-level review + treeUtils unit tests are the current safety net.
- Visual polish: drop indicator styling (color, animation duration).

## Out of Scope (separate tickets)

- `LogsView.svelte:5` imports missing `$lib/components/ui/button.svelte`. Pre-existing issue from commit 0ec2f9cc. Not introduced by this PR.
- `CompositionBuilder.svelte` has its own tree structure (`{ operation, sources }`). Could share `SortableTree` pattern — separate scope if user wants.

---

## PR #3 (2026-07-18)

**Merge commit:** ecfb19d — feat(web): add drag-and-drop to CompositionBuilder (Composite mode)

## Features

### Drag-and-drop in Composite mode (Combine VMs) (#3)
Extends drag-and-drop from PR #2 to the Composite editing mode (Combine VMs). The Leaf editing mode was covered in PR #2; the Composite editor (`CompositionBuilder.svelte`) was deliberately out of scope then and is now in scope per user request.

**What works now:**
- Drag handles appear on every source in a composite operation when `sources.length > 1`
- Drag any source to reorder within its operation
- Nested operations (union of unions) — each recursion level gets its own sortable scope
- Cross-level drag still impossible (correct: each `SortableTree` is scoped to its parent)
- Single-source guard: handle hidden when only 1 source (no meaningful reorder)

**Modified:**
- `web/src/components/CompositionBuilder.svelte` — added `SortableTree` + `SortableItem` + `DragHandle` + `ensureSourceIds` + `handleDragEnd` (flat splice pattern, same as SortBuilder)

## Bug Fixes

### Fix @dnd-kit ghost dependency (#3)
PR #2 imported from `@dnd-kit/dom` and `@dnd-kit/svelte` but did NOT declare them in `web/package.json`. The packages existed in `node_modules` (someone ran `npm install` locally) but a fresh clone + `npm install` would fail with module-not-found.

**Fixed**: Added to `web/package.json` dependencies:
- `"@dnd-kit/dom": "^0.5.0"`
- `"@dnd-kit/svelte": "^0.5.0"`

`web/package-lock.json` regenerated.

## Technical Notes

- Each recursive `CompositionBuilder` instance creates its own `SortableTree` (own `DragDropProvider`). Sources at each level are an independent sortable scope. Cross-level drag is impossible by design.
- `ensureSourceIds()` assigns `__id` to sources lacking one (stable identity for dnd-kit). Called via `{@const _ = ensureSourceIds()}` on render.
- `handleDragEnd()` does immutable flat splice on `node.sources` — same pattern as SortBuilder.
- 7/7 test scenarios pass at code level (S1 single source, S2 multiple sources, S3 nested ops, S4 Leaf mode regression, S5 mode switch, S6 VM ref + nested op, S7 remove source).

## Out of Scope (still pending)

- `LogsView.svelte:5` imports missing `$lib/components/ui/button.svelte`. Pre-existing from commit 0ec2f9cc. Not introduced by PR #2 or PR #3.
- Cursor-relative drop positioning (deferred from PR #2).
- Playwright E2E (no test infra in project).
