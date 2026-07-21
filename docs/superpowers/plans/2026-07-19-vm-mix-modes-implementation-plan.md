# Implementation Plan: VM Mix-Modes (Inline Filter Sources in Composition Tree)

**Spec:** `docs/superpowers/specs/2026-07-19-vm-mix-modes-design.md`
**Branch:** `feature/vm-mixed-modes`
**Worktree:** `.worktrees/vm-mixed-modes`
**Date:** 2026-07-19

---

## Goal

Add inline filter source as third `CompositionNode` flavor alongside existing VM-ref and operation nodes. Users can mix VM references and inline filter expressions in one composition tree, eliminating the leaf/composite toggle. Zero DB migration — JSON column already flexible. Backward-compatible: existing leaf VMs (`composition IS NULL`) untouched.

## Architecture Summary

- **Data model:** No struct changes. Third flavor identified by `vm="" && operation="" && filter_expr!=nil`. New `IsFilterSource()` helper method.
- **Validation:** Relax `ValidateCompositionNode` — allow node with only `filter_expr`. Add filter source-specific rule: no children.
- **Resolution:** New `resolveFilterSource()` in service layer. Extract shared `resolveModelsFiltered()` from `resolveLeafModels` to avoid ~90 lines duplication. `evaluateCompositionNode` gets third branch.
- **API:** No endpoint changes. Existing `Create`/`Update`/`Preview` accept richer JSON. Filter source `filter_expr` validated before `ValidateCompositionNode` call.
- **UX:** Remove mode toggle. Always show composition editor. Auto-convert leaf VMs to composition on edit. New "+ Filter Source" button + rendering branch in `CompositionBuilder`. `formatComposition` handles new node type.

Full details in spec sections 3–6.

---

## Steps

### Step 1: Add `IsFilterSource()` helper + update `ValidateCompositionNode`

**Files:**
- `internal/models/virtual_model.go` — Add `IsFilterSource()` method (after line 136). Modify `ValidateCompositionNode` lines 171-179: replace "must have vm or operation" with "must have vm, operation, or filter_expr". Add filter source validation: `Sources` must be empty.

**Complexity:** S

**Acceptance criteria:**
- `CompositionNode{FilterExpr: &FilterNode{Key: "mc.coding", Op: "gte", Value: 8}}.IsFilterSource()` returns `true`
- `CompositionNode{Vm: "a"}.IsFilterSource()` returns `false`
- `CompositionNode{Operation: "union", Sources: []}.IsFilterSource()` returns `false`
- `ValidateCompositionNode(&CompositionNode{}, 0)` still returns error (empty node — no vm, no op, no filter_expr)
- `ValidateCompositionNode(&CompositionNode{FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"}}, 0)` returns nil
- `ValidateCompositionNode(&CompositionNode{FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"}, Sources: []CompositionNode{{Vm: "a"}}}, 0)` returns error ("filter source node cannot have sources")
- Mixed tree `union(VM-ref, filter-source)` validates successfully

**Tests to add (AT this step):**
- `internal/models/composition_test.go` — Add test cases to existing `TestValidateCompositionNode` table:
  - `"valid filter source"` — FilterExpr set, no vm, no op → passes
  - `"filter source with sources"` — FilterExpr + Sources → fails
  - `"empty node (no vm, no op, no filter_expr)"` — already exists, confirm still fails
  - `"vm and filter_expr (post-filter)"` — passes (existing behavior, vm takes priority)
  - `"mixed tree: union(VM-ref, filter-source)"` — passes
  - `"filter source at nested depth"` — passes
- `internal/models/composition_test.go` — New test `TestIsFilterSource`:
  - filter source → true
  - vm ref → false
  - operation → false
  - nil receiver → false (handle gracefully or panic — decide based on existing pattern)

**Dependencies:** None (first step)
**Risk:** Low. Validation is additive — relaxing a constraint. Existing "empty node" test still passes.

---

### Step 2: Extract `resolveModelsFiltered` from `resolveLeafModels`

**Files:**
- `internal/service/virtual_model_service.go` — Extract lines 226-361 (filter+sort logic) into new `resolveModelsFiltered(ctx, filterExpr, sortExpr, includeModels)` function. Both `resolveLeafModels` and (later) `resolveFilterSource` call it.

**Complexity:** M

**Acceptance criteria:**
- `resolveLeafModels` still works identically (calls `resolveModelsFiltered` with same args)
- New function signature: `func (s *virtualModelService) resolveModelsFiltered(ctx context.Context, filterExpr models.FilterNode, sortExpr models.SortExpr, includeModels json.RawMessage) ([]ResolvedModel, error)`
- All existing tests pass without modification

**Tests to add/modify:**
- No new tests — this is pure refactor. Existing tests in `internal/service/composition_test.go` must still pass.
- After refactor, run: `rtk go test ./internal/service/... -v` — all green.

**Dependencies:** None (can parallel with Step 1)
**Risk:** Medium. Extract carefully. `resolveLeafModels` currently parses `vm.FilterExpr` (json.RawMessage) → `models.FilterNode`. The new function should accept already-parsed structs, not raw JSON. Verify the leaf path still unmarshals correctly before calling.

---

### Step 3: Add `resolveFilterSource` + wire into `evaluateCompositionNode`

**Files:**
- `internal/service/virtual_model_service.go` — Add `resolveFilterSource` function. Add third branch in `evaluateCompositionNode` (line 372): `if node.IsFilterSource() { return s.resolveFilterSource(ctx, node) }`. Update error message to "must have vm, operation, or filter_expr".

**Complexity:** M

**Acceptance criteria:**
- `resolveFilterSource` calls `resolveModelsFiltered` with `node.FilterExpr`, `node.SortExpr`, `nil` (no include_models)
- `evaluateCompositionNode` correctly dispatches to `resolveFilterSource` for filter source nodes
- Empty filter result returns empty slice (not error)
- Sort on filter source applied correctly

**Tests to add:**
- `internal/service/composition_test.go` — New test functions (need mock repos or use integration-style with real service):
  - `TestResolveFilterSource_BasicFilter` — filter matches subset of models → correct results
  - `TestResolveFilterSource_EmptyResult` — filter matches nothing → empty slice
  - `TestResolveFilterSource_WithSort` — sort_expr applied to filtered results
  - `TestResolveFilterSource_InUnion` — `union(VM-ref, filter-source)` returns combined set
  - `TestResolveFilterSource_InIntersection` — intersection with filter source
  - `TestResolveFilterSource_InDifference` — difference with filter source
  - `TestResolveFilterSource_NestedInOperation` — filter source inside nested operation tree
  - `TestResolveFilterSource_CircularRefNotPossible` — filter source has no vm field, no cycle possible

  Note: These tests need a `virtualModelService` with mock repos. Check if existing test patterns use mocks or integration setup. Adapt accordingly.

**Dependencies:** Step 2 (needs `resolveModelsFiltered`)
**Risk:** Medium. Test setup complexity depends on existing mock patterns. If no mocks exist, may need to create minimal mock repos.

---

### Step 4: Service-layer validation for filter source nodes

**Files:**
- `internal/service/virtual_model_service.go` — In `Create` (line 63-74) and `Update` (line 130-133) and `PreviewResolve` (line 180-191): when composition is set, validate `filter_expr` on filter source nodes before calling `ValidateCompositionNode`. Walk the tree, for each `IsFilterSource()` node, call `validateFilterExpr` on its `FilterExpr` (serialize to json.RawMessage first, or validate directly).

**Complexity:** S

**Acceptance criteria:**
- `POST /api/v1/virtual-models` with composition containing filter source with invalid filter_expr → 400 error
- `POST /api/v1/virtual-models` with composition containing filter source with valid filter_expr → 201
- Existing leaf VM create/update unchanged

**Tests to add:**
- `internal/service/composition_test.go` — Integration test:
  - `TestCreate_InvalidFilterSourceFilterExpr` — rejects invalid filter in filter source
  - `TestCreate_ValidFilterSourceFilterExpr` — accepts valid filter in filter source

**Dependencies:** Step 1 (needs updated validation) + Step 3 (needs `resolveFilterSource`)
**Risk:** Low. Pattern already exists for leaf VM filter validation.

---

### Step 5: CompositionBuilder — add filter source rendering + button

**Files:**
- `web/src/components/CompositionBuilder.svelte`:
  - Add `addFilterSource()` function (push `{ filter_expr: { and: [] }, sort_expr: [] }` to `node.sources`)
  - Add "+ Filter Source" button alongside existing "+ Source" and "+ Nested Op" buttons (after line 269)
  - Add `switchToFilterSource()` function (clear vm/operation, set filter_expr)
  - Add `switchFromFilterSource()` functions for →ref and →op transitions
  - Add third rendering branch: `{:else if node.filter_expr !== undefined && !node.vm && !node.operation}` — render filter source node with amber border, ConditionBuilder, SortBuilder, switch buttons
  - Follow existing Svelte 5 patterns: `onChange` prop (camelCase, not `onchange`), `$state`, `$props`

**Complexity:** M

**Acceptance criteria:**
- Clicking "+ Filter Source" adds a new filter source node to sources array
- Filter source node renders with amber left border, "Filter Source" label
- Filter source node shows expandable ConditionBuilder (F button) and SortBuilder (S button)
- "→ref" button converts filter source to VM ref node
- "→op" button converts filter source to operation node
- VM ref node and operation node unchanged
- All existing functionality works

**Tests to add:**
- No existing web test framework (glob returned 0 test files). Manual verification or note for future.
- If adding tests: `web/src/components/CompositionBuilder.test.svelte` or similar — but spec says "component tests" — check if project has vitest/jest configured.

**Dependencies:** None (UI work, can parallel with Steps 1-4)
**Risk:** Low-Medium. Svelte 5 `$state` reactivity with array mutation (`push`, `splice`) — verify works with Svelte 5 runes (it does per Svelte 5 docs).

---

### Step 6: VirtualModelForm — remove toggle, auto-convert leaf→composition

**Files:**
- `web/src/components/VirtualModelForm.svelte`:
  - Remove `mode` state variable (line 14)
  - Remove mode toggle buttons (lines 179-194)
  - Remove `{#if mode === 'leaf'}...{:else}...{/if}` block (lines 196-217)
  - Replace with single `<CompositionBuilder bind:node={compositionNode} />`
  - In `loadVM()`: if `vm.composition` is null but `vm.filter_expr` exists, auto-convert: `compositionNode = { filter_expr: vm.filter_expr, sort_expr: vm.sort_expr || [] }`
  - In `handleSubmit()`: remove `if (mode === 'composite')` branch. Always send `body.composition = compositionNode`
  - Update `isValidComposition()` to handle filter source nodes (valid if `filter_expr` set, no vm, no operation)
  - Update preview: always pass `composition` to `ResolvedPreview`

**Complexity:** M

**Acceptance criteria:**
- No mode toggle visible in create or edit form
- Editing existing leaf VM auto-loads filter/sort into composition tree
- Saving always sends `composition` field
- Preview works with composition-only input
- Editing existing composite VM works unchanged

**Tests to add:**
- Manual verification (no existing web test framework)

**Dependencies:** Step 5 (needs CompositionBuilder to support filter source nodes)
**Risk:** Medium. Auto-convert logic must handle edge cases: empty filter_expr, null sort_expr. Verify `ResolvedPreview` accepts `composition` prop correctly (check existing usage).

---

### Step 7: VirtualModelList — formatComposition for filter source nodes

**Files:**
- `web/src/components/VirtualModelList.svelte`:
  - Modify `formatComposition` (lines 71-81): add branch after `if (node.vm)` check: `if (node.filter_expr && !node.vm && !node.operation)` → show "Filter: {summary}" with truncation at 60 chars
  - Add helper `formatFilter` already exists — reuse for summary
  - `formatSort` already exists — reuse for sort summary

**Complexity:** S

**Acceptance criteria:**
- List view shows "Filter: mc.coding gte 8" for filter source nodes
- Long summaries truncated at 60 chars with "..."
- VM ref and operation nodes render unchanged
- Existing leaf VMs (no composition) still show filter_expr directly

**Tests to add:**
- Manual verification

**Dependencies:** None (independent of other UI changes)
**Risk:** Low. Simple conditional addition to existing function.

---

### Step 8: Backward compatibility — leaf VM still works end-to-end

**Files:**
- No file changes. Verification step.

**Complexity:** S (testing only)

**Acceptance criteria:**
- `POST /api/v1/virtual-models` with top-level `filter_expr` + `sort_expr` (no composition) → 201, `composition` is null in response
- `GET /api/v1/virtual-models/:id` for existing leaf VM → returns same format as before
- `PUT /api/v1/virtual-models/:id` with top-level filter_expr → works, composition stays null
- `GET /api/v1/virtual-models/:id/preview` for leaf VM → correct resolved models

**Tests to add:**
- Integration test confirming leaf path unchanged

**Dependencies:** Steps 1-4 (backend complete)
**Risk:** Low. Existing behavior preserved by design.

---

### Step 9: Integration tests — create→preview→resolve flow

**Files:**
- `internal/service/composition_test.go` or new `internal/service/virtual_model_integration_test.go`

**Complexity:** M

**Acceptance criteria (all must pass):**
- Create VM with `composition: { operation: "union", sources: [{ vm: "a" }, { filter_expr: { and: [{ key: "mc.coding", op: "gte", value: "8" }] } }] }` → resolves correctly
- Edit existing leaf VM → auto-converts to composition → save → roundtrip works
- Composition tree with mix of VM refs and filter sources → preview returns correct merged results
- `GET /api/v1/virtual-models` → `formatComposition` handles all node types (manual/UI test)

**Tests to add:**
- `TestIntegration_CreateWithFilterSource` — full create→get→resolve cycle
- `TestIntegration_LeafToCompositionConvert` — update leaf VM to composition with filter source
- `TestIntegration_MixedTreeResolve` — complex tree with VM refs, operations, and filter sources at various depths

**Dependencies:** Steps 1-7 (all code complete)
**Risk:** Medium. Integration test setup requires mock repos or test DB.

---

### Step 10: Edge case tests (from spec section 8)

**Files:**
- `internal/models/composition_test.go` — validation edge cases
- `internal/service/composition_test.go` — resolution edge cases

**Complexity:** M

**Acceptance criteria (each a separate test):**
- 8.1: Empty filter result in set op → `union(VM-A, empty)` = VM-A's models; `intersection(VM-A, empty)` = empty; `difference(VM-A, empty)` = VM-A's models
- 8.2: Composite-of-composite with inline filter → resolves correctly at any depth
- 8.3: Sort tie-breaking → parent sort overrides inline source sort after set op
- 8.5: Depth limit → filter source counts as one level, depth 10 with filter sources valid
- 8.6: No `include_models` on filter sources → verify CompositionNode has no such field (struct check)
- 8.7: Per-node filter_expr on parent of inline source → AND-merge semantics, parent filter post-filters output

**Tests to add:**
- `TestEdgeCase_EmptyFilterInUnion`
- `TestEdgeCase_EmptyFilterInIntersection`
- `TestEdgeCase_EmptyFilterInDifference`
- `TestEdgeCase_CompositeOfCompositeWithInlineFilter`
- `TestEdgeCase_ParentSortOverridesInlineSort`
- `TestEdgeCase_FilterSourceAtDepthLimit`
- `TestEdgeCase_ParentFilterOnInlineSource`

**Dependencies:** Steps 1-4 (backend complete)
**Risk:** Low. Well-defined edge cases from spec.

---

## Testing Strategy Summary

| Layer | When | What |
|-------|------|------|
| **Model validation** | Step 1 | Table-driven tests for new validation rules |
| **Service refactor** | Step 2 | Existing tests still pass (refactor-only) |
| **Service resolution** | Step 3 | New filter source resolve tests |
| **Service validation** | Step 4 | Filter source filter_expr validation |
| **Backward compat** | Step 8 | Leaf VM path unchanged |
| **Integration** | Step 9 | Create→preview→resolve flow |
| **Edge cases** | Step 10 | All spec section 8 scenarios |
| **UI** | Steps 5-7 | Manual (no web test framework exists) |

All backend tests added AT the step that introduces the change. No deferred testing.

---

## Rollout / Verification Checklist

- [ ] All Go tests pass: `rtk go test ./... -v`
- [ ] All existing Go tests still pass (no regression)
- [ ] Web build succeeds: `rtk npm run build` (in `web/`)
- [ ] Manual UI verification: create VM with filter source, edit leaf VM, list view
- [ ] No DB migration needed (verify composition column is JSON, no schema change)
- [ ] No new dependencies added
- [ ] ADR-001 compatible (semantics extended, not replaced)
- [ ] Branch: `feature/vm-mixed-modes`
- [ ] Worktree: `.worktrees/vm-mixed-modes`

---

## Out-of-Scope (Explicit)

- No DB migration
- No new API endpoints
- No ADR change (fits existing ADR-001)
- No explicit display name field on CompositionNode (auto-generated summary, truncate at 60 chars)
- No `include_models` on filter sources
- No inline filter source as top-level node (equivalent to leaf VM)
- No bulk migration tool for leaf→composition conversion
- No renaming VMs or changing identity semantics
- No auth/permission changes
- No depth limit change (stays at 10)
- No nested inline filter sources (leaf nodes have no children)

---

## Estimated Complexity

| Step | Size |
|------|------|
| 1. IsFilterSource + validation | S |
| 2. Extract resolveModelsFiltered | M |
| 3. resolveFilterSource + wire | M |
| 4. Service validation | S |
| 5. CompositionBuilder UI | M |
| 6. VirtualModelForm UI | M |
| 7. VirtualModelList UI | S |
| 8. Backward compat verification | S |
| 9. Integration tests | M |
| 10. Edge case tests | M |

**Total: 4S + 6M = ~10 units**

---

## Parallelization Opportunities

- Steps 1 + 2 can run in parallel (model layer vs service refactor)
- Steps 5 + 7 can run in parallel (CompositionBuilder vs VirtualModelList)
- Steps 3 + 5 can run in parallel (service resolution vs UI)
- Steps 9 + 10 can run in parallel (integration vs edge cases)
- Step 6 depends on Step 5 (needs filter source support in CompositionBuilder)

**Critical path:** 1 → 3 → 4 → 8 → 9 (backend chain)

---

## Spec Gaps / Concerns Found During Decomposition

1. **No web test framework exists.** Glob for `web/src/**/*.test.*` returned 0 files. Spec section 9.3 lists component tests (`CompositionBuilder.test.svelte`) but no vitest/jest config found. **Decision needed:** either add test framework (out-of-scope for this feature) or accept manual-only UI verification. Recommend manual for now, add framework in separate effort.

2. **Mock repos needed for Steps 3/9/10.** Existing service tests (`composition_test.go`) only test pure functions (`filterResolvedModels`, set operations) with `&virtualModelService{}`. `resolveFilterSource` → `resolveModelsFiltered` needs 6 repo interfaces: `ModelRepository.ListAll`, `TagRepository.GetAvailableEfforts/GetByModelEffort`, `ProviderRepository.GetByID`, `ProviderMetadataRepository.GetByProvider`, `GlobalMetadataRepository.GetByModelEffort`. No mock implementations exist. **Engineer must create minimal mock repos** in `internal/service/mock_repos_test.go` (or similar) before writing Step 3 tests. Spec doesn't mention this. Add to Step 3 scope.

3. **`validateFilterExpr` takes `json.RawMessage` but filter source has `*FilterNode` (struct).** Step 4 validation: either (a) marshal `FilterNode` → JSON → call existing `validateFilterExpr`, or (b) write new `validateFilterNode(*FilterNode)` — wait, `validateFilterNode` already exists (line 867). Just call it directly on `node.FilterExpr`. No marshal needed. Cleaner than spec suggests.

4. **Spec line 127 says "validate in service layer before calling ValidateCompositionNode".** But `ValidateCompositionNode` is model-layer. Service layer calls it. So: walk composition tree in service layer, call `validateFilterNode` on each filter source's `FilterExpr`, THEN call `ValidateCompositionNode` for structural validation. Order matters.

5. **Auto-convert edge case: leaf VM with empty filter_expr.** Spec says auto-convert when `vm.filter_expr` exists. But what if `filter_expr` is `{}` or `null`? `loadVM` must handle: `{}` → create filter source with `{ and: [] }`; `null` → don't convert (but this shouldn't happen — leaf VM always has filter_expr). Test explicitly.

6. **`ResolvedPreview` prop change.** Currently VirtualModelForm passes either `filterExpr` OR `composition` to ResolvedPreview. After removing toggle, always pass `composition`. Verify ResolvedPreview handles `composition` prop correctly — it already does (line 272: `<ResolvedPreview previewMode={true} composition={compositionNode} />`). Just remove the leaf branch.

7. **No circular ref concern for filter sources.** Spec section 8.4 confirms. But `resolveFilterSource` should NOT accept/use `stack` parameter (don't pass cycle-detection to a function that can't cycle). Clean API boundary.
