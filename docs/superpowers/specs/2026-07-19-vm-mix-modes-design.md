# Inline Leaf Sources in Composition Tree

**Date:** 2026-07-19
**Status:** Draft

## 1. Context & Motivation

Virtual models today have a **binary choice**: either a flat leaf (filter_expr + sort_expr over all models) or a composite (tree of VM references + set operations). Users cannot combine both in one VM.

**Problem:** A user who wants "union of VM-A with models matching coding ≥ 8" must create two separate VMs — one for the filter, one for the union — then manually coordinate them. The leaf/composite toggle in the UI reinforces this artificial split.

**Goal:** Unify into a single composition tree where any leaf can be either a VM reference OR an inline filter source. Remove the toggle entirely. One editor, one model.

## 2. Goals & Non-Goals

### Goals

- Add inline filter source as a third `CompositionNode` flavor (filter_expr + sort_expr, no vm, no operation)
- Remove leaf/composite mode toggle from VirtualModelForm
- Single unified composition editor for all VMs
- Zero DB migration (composition column is JSON, already flexible)
- Backward compatible: existing leaf VMs (composition IS NULL) continue working unchanged

### Non-Goals

- Renaming VMs or changing VM identity semantics
- Changing depth limit (stays at 10)
- Auth/permission changes
- Inline filter sources referencing external data sources (only model tags/metadata)
- Nested inline filter sources inside other inline filter sources (leaf nodes have no children)
- Merging leaf VMs into composition VMs automatically (user-initiated only)

## 3. Data Model

### Current CompositionNode (no changes to struct)

```go
type CompositionNode struct {
    Vm        string            `json:"vm,omitempty"`
    Operation string            `json:"operation,omitempty"` // "union", "intersection", "difference"
    Sources   []CompositionNode `json:"sources,omitempty"`
    FilterExpr *FilterNode      `json:"filter_expr,omitempty"`
    SortExpr   SortExpr         `json:"sort_expr,omitempty"`
}
```

**No struct changes needed.** The third flavor is identified by the absence of both `vm` and `operation`, with `filter_expr` present. The existing `FilterExpr` and `SortExpr` fields serve double duty:

| Flavor | vm | operation | filter_expr | sources |
|--------|----|-----------|-------------|---------|
| VM reference | set | empty | optional (post-filter) | empty |
| Operation | empty | set | optional (post-filter) | ≥2 children |
| **Inline filter source** (NEW) | empty | empty | **required** | empty |

### New helper method

```go
func (n *CompositionNode) IsFilterSource() bool {
    return n.Vm == "" && n.Operation == "" && n.FilterExpr != nil
}
```

### JSON example: union(VM-A, inline filter source)

```json
{
  "operation": "union",
  "sources": [
    { "vm": "smart-free" },
    {
      "filter_expr": { "and": [
        { "key": "mc.coding", "op": "gte", "value": "8" }
      ]},
      "sort_expr": [{ "key": "mc.cost_per_task", "direction": "asc" }]
    }
  ]
}
```

## 4. Validation Changes

### File: `internal/models/virtual_model.go`, function `ValidateCompositionNode`

**Current rule (lines 174-178):**
```go
if hasVM && hasOp {
    return fmt.Errorf("composition node cannot have both vm and operation")
}
if !hasVM && !hasOp {
    return fmt.Errorf("composition node must have either vm or operation")
}
```

**New rule — replace lines 171-179 with:**
```go
hasVM := node.Vm != ""
hasOp := node.Operation != ""

if hasVM && hasOp {
    return fmt.Errorf("composition node cannot have both vm and operation")
}
if !hasVM && !hasOp && node.FilterExpr == nil {
    return fmt.Errorf("composition node must have vm, operation, or filter_expr")
}
if !hasVM && !hasOp && node.FilterExpr != nil {
    // Inline filter source — no children allowed
    if len(node.Sources) > 0 {
        return fmt.Errorf("filter source node cannot have sources")
    }
}
```

**Node flavor determination:**

| vm set? | operation set? | filter_expr set? | Node flavor |
|---------|---------------|-----------------|-------------|
| yes | no | any | VM reference (filter_expr = post-filter) |
| no | yes | any | Operation (filter_expr = post-filter) |
| no | no | yes | **Inline filter source** (NEW) |
| no | no | no | **Invalid** |
| yes | yes | any | **Invalid** |

`filter_expr` serves dual role: discriminator for inline filter sources, post-filter for VM-ref/operation nodes. Unambiguous — `vm` or `operation` presence takes priority.

**Filter source-specific validation:**
- `Sources` must be empty (leaf node, no children)
- `FilterExpr` must be valid — validate in service layer `Create`/`Update` (call existing `validateFilterExpr`) before calling `ValidateCompositionNode`

**Backward compatibility:** VM-ref nodes with post-filter_expr still work exactly as before. The `vm` check runs first — if `vm` is set, the node is a VM ref regardless of `filter_expr`.

## 5. Resolution Logic

### File: `internal/service/virtual_model_service.go`

#### 5.1 `evaluateCompositionNode` (line 364-373)

**Current:**
```go
func (s *virtualModelService) evaluateCompositionNode(ctx context.Context, node *models.CompositionNode, stack map[string]bool) ([]ResolvedModel, error) {
    if node.IsVMRef() {
        return s.resolveVMRef(ctx, node, stack)
    }
    if node.IsOperation() {
        return s.resolveOperation(ctx, node, stack)
    }
    return nil, fmt.Errorf("composition node must have either vm or operation")
}
```

**New:**
```go
func (s *virtualModelService) evaluateCompositionNode(ctx context.Context, node *models.CompositionNode, stack map[string]bool) ([]ResolvedModel, error) {
    if node.IsVMRef() {
        return s.resolveVMRef(ctx, node, stack)
    }
    if node.IsOperation() {
        return s.resolveOperation(ctx, node, stack)
    }
    if node.IsFilterSource() {
        return s.resolveFilterSource(ctx, node)
    }
    return nil, fmt.Errorf("composition node must have vm, operation, or filter_expr")
}
```

#### 5.2 New `resolveFilterSource` function

Extracts the core filter+sort logic from `resolveLeafModels` (lines 226-362) into a reusable form:

```go
func (s *virtualModelService) resolveFilterSource(ctx context.Context, node *models.CompositionNode) ([]ResolvedModel, error) {
    // 1. List all models
    allModels, err := s.modelRepo.List(ctx)
    if err != nil {
        return nil, fmt.Errorf("list models: %w", err)
    }

    // 2. Parse filter
    var filter *models.FilterNode
    if node.FilterExpr != nil {
        filter = node.FilterExpr
    }

    // 3. Iterate all models, match filter
    var resolved []ResolvedModel
    for _, m := range allModels {
        // For each reasoning effort variant...
        efforts := m.ReasoningEfforts
        if len(efforts) == 0 {
            efforts = []string{""}
        }
        for _, effort := range efforts {
            // Fetch tags, provider, metadata (same logic as resolveLeafModels lines 270-320)
            // Evaluate matchesFilter()
            // If match, append to resolved
        }
    }

    // 4. Apply sort
    if len(node.SortExpr) > 0 {
        sort.Slice(resolved, func(i, j int) bool {
            return compareModels(resolved[i], resolved[j], node.SortExpr)
        })
    }

    return resolved, nil
}
```

**Implementation note:** To avoid duplicating ~90 lines from `resolveLeafModels`, extract a shared `resolveModelsFiltered(ctx, filterExpr, sortExpr, includeModels)` helper. Both `resolveLeafModels` and `resolveFilterSource` call it. `resolveLeafModels` passes `vm.FilterExpr`, `vm.SortExpr`, `vm.IncludeModels`. `resolveFilterSource` passes `node.FilterExpr`, `node.SortExpr`, `nil` (no include_models for inline sources).

#### 5.3 Example trace: `union(VM-A, filter_source{coding ≥ 8})`

```
ResolveModels(vm-with-composition)
  → vm.Composition != nil → evaluateCompositionNode(root)
    → root.IsOperation() → resolveOperation(root, stack)
      → source[0]: evaluateCompositionNode(VM-A-ref)
        → IsVMRef() → resolveVMRef(VM-A-ref, stack)
          → GetByName("VM-A") → ResolveModels(VM-A)  [recursive, VM-A is leaf]
            → resolveLeafModels(VM-A) → [model1, model2, model3]
          → apply VM-A-ref's per-node filter/sort (if any)
          → return [model1, model2, model3]
      → source[1]: evaluateCompositionNode(filter-source)
        → IsFilterSource() → resolveFilterSource(filter-source)
          → list all models → filter by coding ≥ 8 → [model2, model4, model5]
          → sort by cost_per_task asc
          → return [model2, model4, model5]
      → applySetOperation("union", [[model1,2,3], [model2,4,5]])
        → [model1, model2, model3, model4, model5]
      → apply root's per-node filter/sort (if any)
      → return [model1, model2, model3, model4, model5]
```

## 6. UX Changes

### 6.1 Remove mode toggle — `VirtualModelForm.svelte`

**Delete:** Lines 179-194 (the mode toggle buttons: "Leaf (Filter/Sort)" and "Composite (Combine VMs)").

**Delete:** The `mode` state variable (line 14: `let mode = $state('leaf')`).

**Replace:** The `{#if mode === 'leaf'}...{:else}...{/if}` block (lines 196-217) with a single `<CompositionBuilder>` component.

**Modify `handleSubmit`** (lines 87-135): Remove the `if (mode === 'composite')` branch. Always send `body.composition = compositionNode`. Never send top-level `filter_expr` or `sort_expr`.

**Migration of existing state:** When editing an existing leaf VM (`composition IS NULL`, has `filter_expr`/`sort_expr`), auto-convert to a single-node composition tree on form load:

```javascript
// On mount, if vm has filter_expr but no composition:
if (!vm.composition && vm.filter_expr) {
    compositionNode = {
        filter_expr: vm.filter_expr,
        sort_expr: vm.sort_expr || []
    };
}
```

### 6.2 New node type button — `CompositionBuilder.svelte`

**Add** a third button alongside "+ Source" and "+ Nested Op" (lines 261-269):

```svelte
<button
    class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
    onclick={addFilterSource}
>+ Filter Source</button>
```

**New function:**
```javascript
function addFilterSource() {
    if (!node.sources) node.sources = [];
    node.sources.push({ filter_expr: { and: [] }, sort_expr: [] });
    emit();
}
```

### 6.3 Inline filter source node rendering — `CompositionBuilder.svelte`

**Add** a third rendering branch (after VM-ref and operation branches):

```svelte
{:else if node.filter_expr !== undefined && !node.vm && !node.operation}
    <!-- Inline Filter Source node -->
    <div class="border-l-2 border-l-amber-500 pl-3 space-y-2">
        <div class="flex items-center gap-2 mb-2">
            <span class="text-amber-400 text-xs font-medium">Filter Source</span>
            <button ... onclick={() => expandedFilter = !expandedFilter}>F</button>
            <button ... onclick={() => expandedSort = !expandedSort}>S</button>
            <button ... onclick={() => switchToVMRef()}>→ref</button>
            <button ... onclick={() => switchToOp()}>→op</button>
        </div>
        {#if expandedFilter}
            <ConditionBuilder node={node.filter_expr || { and: [] }} onChange={(v) => updateNodeFilter(v)} />
        {/if}
        {#if expandedSort}
            <SortBuilder criteria={node.sort_expr || []} onChange={(v) => updateNodeSort(v)} />
        {/if}
    </div>
```

### 6.4 List view rendering — `VirtualModelList.svelte`

**Modify `formatComposition`** (lines 71-81) to handle filter source nodes:

```javascript
function formatComposition(node, depth = 0) {
    if (!node) return '';
    if (node.vm) return node.vm;
    if (node.filter_expr && !node.vm && !node.operation) {
        // Inline filter source — show summary
        const summary = formatFilter(node.filter_expr);
        const sortSummary = node.sort_expr?.length ? `, ${formatSort(node.sort_expr)}` : '';
        const full = `Filter: ${summary}${sortSummary}`;
        return full.length > 60 ? full.slice(0, 57) + '...' : full;
    }
    if (node.operation) {
        const opSymbol = { union: '∪', intersection: '∩', difference: '\\' }[node.operation] || node.operation;
        const children = (node.sources || []).map(s => formatComposition(s, depth + 1));
        if (depth === 0) return `${node.operation}: ${children.join(' ')} ${opSymbol}`;
        return `(${children.join(` ${opSymbol} `)})`;
    }
    return '?';
}
```

### 6.5 Display name (Q1 decision)

No explicit name field on `CompositionNode`. Display is auto-generated from filter_expr + sort_expr content via `formatComposition`. If the summary exceeds 60 characters, truncate with ellipsis: `"Filter: coding ≥ 8, intel..."`. This avoids schema changes while providing meaningful display in list view and dep graph.

## 7. Migration Strategy

**Zero DB migration.** Two-pronged backward compatibility:

### 7.1 Existing leaf VMs (composition IS NULL)

Keep working unchanged. The `ResolveModels` function (line 208-223) already handles this:

```go
if vm.Composition != nil {
    // composite path
} else {
    return s.resolveLeafModels(ctx, vm)  // existing leaf path, untouched
}
```

No code change here. `composition IS NULL` remains the "implicit single filter source" representation. The API continues to accept `filter_expr` + `sort_expr` at the top level for leaf VMs.

### 7.2 API backward compatibility

**Create/Update endpoints:** Continue accepting both formats:

- `{"composition": {...}}` → composition mode (new unified path)
- `{"filter_expr": {...}, "sort_expr": [...]}` → leaf mode (existing path, composition stays NULL)

**No forced migration.** Existing leaf VMs work forever. Users convert to composition tree only when they need inline filter sources. The UI auto-converts on edit (section 6.1), but the save still goes through the existing API — if the user only has a single filter source with no VM refs, the UI can either:

- **Option A:** Save as `composition: { filter_expr: ..., sort_expr: ... }` (new format, one-time conversion on first edit)
- **Option B:** Save as top-level `filter_expr` + `sort_expr` (preserve existing format, no conversion)

**Decision: Option A.** When the unified editor saves a single-node composition that is a filter source, it sends it as `composition`. This is a one-way upgrade on first edit. The old leaf format is still accepted for API backward compat but the UI no longer creates it.

### 7.3 API response

The list/detail endpoints already return `composition` when it exists. No change needed.

## 8. Edge Cases

### 8.1 Empty filter result in set operation

If an inline filter source matches zero models, the set operation proceeds with an empty set:

- `union(VM-A, empty)` → VM-A's models
- `intersection(VM-A, empty)` → empty
- `difference(VM-A, empty)` → VM-A's models

This is consistent with current behavior for VM references that resolve to empty.

### 8.2 Composite-of-composite with inline filter

Inline filter sources can appear at any depth in the tree. Example:

```json
{
  "operation": "union",
  "sources": [
    {
      "operation": "intersection",
      "sources": [
        { "vm": "planning" },
        { "filter_expr": { "and": [{ "key": "mc.coding", "op": "gte", "value": "7" }] } }
      ]
    },
    { "vm": "chat" }
  ]
}
```

Resolution recurses normally. No special handling needed.

### 8.3 Sort tie-breaking

When inline filter source has `sort_expr` and the parent operation node also has `sort_expr`, the parent sort wins (applied after set operation). Same as current VM-ref behavior: per-node sort on the resolved result, then parent sorts the combined output.

### 8.4 Circular ref impossibility

Inline filter sources cannot create circular references — they have no `vm` field, so they never trigger VM lookup. The `stack` cycle-detection mechanism is irrelevant for filter source nodes. `resolveFilterSource` does not recurse through VM resolution.

### 8.5 Depth limit

Inline filter sources count as one level in the depth limit (same as VM refs). A tree of depth 10 with inline filter sources at the leaves is valid.

### 8.6 include_models on inline filter sources

Inline filter sources do NOT support `include_models`. The `CompositionNode` struct has no `IncludeModels` field, and adding one is out of scope. If a user needs specific models included, they should use a VM-ref node with `include_models` on the referenced VM, or use a top-level `include_models` on the composite VM itself.

### 8.7 Per-node filter_expr on top of inline source (Q3 decision)

A VM-ref node can have both `vm` and `filter_expr` — the `filter_expr` post-filters the VM's output. An inline filter source has `filter_expr` as its primary discriminator. Can a parent operation node also have `filter_expr` that further narrows an inline filter source's output?

**Yes.** Example:

```json
{
  "operation": "union",
  "sources": [
    { "vm": "planning" },
    {
      "filter_expr": { "and": [{ "key": "mc.coding", "op": "gte", "value": "8" }] }
    }
  ],
  "filter_expr": { "and": [{ "key": "mc.cost_type", "op": "eq", "value": "free" }] }
}
```

Resolution:
1. source[0] (VM-ref "planning") → resolves to [planning models]
2. source[1] (inline filter) → resolves to [coding ≥ 8 models]
3. union → [planning models ∪ coding ≥ 8 models]
4. parent filter_expr post-filters → result narrowed to [free models only]

AND-merge semantics: parent filter = narrower slice applied after set operation. Consistent with existing VM-ref + per-node filter behavior.

## 9. Testing Strategy

### 9.1 Unit tests — `internal/models/virtual_model_test.go`

- `TestValidateCompositionNode_FilterSource`: valid filter source node passes
- `TestValidateCompositionNode_VMRefWithPostFilter`: passes (vm + filter_expr = VM ref with post-filter, existing behavior)
- `TestValidateCompositionNode_FilterSourceWithSources`: fails (sources not allowed on filter source)
- `TestValidateCompositionNode_EmptyNode`: fails (no vm, no operation, no filter_expr)
- `TestValidateCompositionNode_VmAndOperation`: fails (vm + operation is invalid)
- `TestValidateCompositionNode_MixedTree`: union(VM-ref, filter-source) passes
- `TestIsFilterSource`: helper returns true/false correctly

### 9.2 Unit tests — `internal/service/virtual_model_service_test.go`

- `TestResolveFilterSource_BasicFilter`: filter returns correct models
- `TestResolveFilterSource_EmptyResult`: filter matches nothing → empty set
- `TestResolveFilterSource_WithSort`: sort applied correctly
- `TestResolveFilterSource_InUnion`: union(VM-A, filter-source) returns correct combined set
- `TestResolveFilterSource_InIntersection`: intersection with filter source works
- `TestResolveFilterSource_InDifference`: difference with filter source works
- `TestResolveFilterSource_NestedInOperation`: filter source inside nested operation
- `TestResolveFilterSource_CircularRefNotPossible`: no cycle detection needed

### 9.3 Component tests — `web/src/components/CompositionBuilder.test.svelte`

- `TestAddFilterSource`: clicking "+ Filter Source" adds correct node shape
- `TestRenderFilterSource`: filter source node renders ConditionBuilder + SortBuilder
- `TestSwitchFilterSourceToVMRef`: "→ref" button converts correctly
- `TestSwitchFilterSourceToOp`: "→op" button converts correctly

### 9.4 Integration tests

- `POST /api/v1/virtual-models` with composition containing inline filter source → 201, resolves correctly
- `PUT /api/v1/virtual-models/:id` converting leaf to composition with filter source → 200
- `GET /api/v1/virtual-models/:id/preview` with inline filter source → correct resolved models
- `GET /api/v1/virtual-models` list → formatComposition shows "Filter: ..." summary

### 9.5 E2E (manual or Playwright)

- Create VM with inline filter source via unified editor → appears in list with summary
- Edit existing leaf VM → auto-converts to composition tree → save → works
- Composition tree with mix of VM refs and filter sources → preview shows correct results

## 10. Open Questions / Future Work

### Deferred

- **Explicit display name field on CompositionNode**: If auto-generated summaries prove insufficient for dep graph readability, add optional `name string` field in a follow-up.
- **include_models on inline filter sources**: Currently unsupported. Would require adding field to CompositionNode struct.
- **Inline filter source as top-level node**: Currently the composition root must be an operation or VM-ref. A single inline filter source at root is equivalent to a leaf VM — no UX benefit. Could revisit if sub-VM filtering becomes a use case.
- **Bulk migration tool**: If many users want to convert leaf VMs to composition format without manual editing, a CLI migration command could be added.

### Resolved

- **Q1 (display name)**: Auto-generated from filter_expr + sort_expr content. No explicit name field. Truncate at 60 chars.
- **Q3 (per-node filter on inline source)**: AND-merge semantics. Parent filter post-filters inline source output. Sort: parent sort overrides inline source sort.
