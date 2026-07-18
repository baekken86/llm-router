# ADR-003: Drag-and-Drop Library for Hierarchical Operation Trees

## Status

Accepted

## Context

The llm-router web UI's condition/sort builders use arrow buttons (`moveUp`/`moveDown`) for reordering. Users want a richer experience: drag handles, drag-and-drop reordering, and reparenting of nodes within the recursive condition tree.

The operation tree has two structural levels:
- **ConditionBuilder**: Recursive AND/OR/NOT tree (groups contain children, children can be groups or leaf conditions)
- **SortBuilder**: Flat list of sort criteria (some with conditional IF branches)

Requirements:
- Drag any node (leaf, intermediate group, top-level)
- Drop positions: before, after, inside (group nodes only)
- Cycle detection (prevent dropping ancestor into descendant)
- Cross-component drag NOT supported (ConditionBuilder ↔ SortBuilder use different data types)
- Immutable tree updates (no mutation)

## Decision

### Library: @dnd-kit/svelte 0.5.0 + @dnd-kit/dom 0.5.0

Already a dependency in package.json. Provides:
- `DragDropProvider` (top-level only, NOT recursive)
- `createSortable` (per-item sortable binding)
- `DragOverlay` (ghost element during drag)
- Pointer + keyboard sensors built in
- No external React dependency (pure Svelte 5)

### Nested Group Strategy

Flat sortable list with visual indentation. Each node gets a `group` identifier matching its parent. dnd-kit sorts within a group; cross-group moves handled in `onDragEnd`:

```
Group "root" → [item-a, group-1, item-b]      (depth 0)
Group "group-1" → [item-c, item-d]            (depth 1)
```

Drag item from group-1 to root: `onDragEnd` receives source group + target group, splices tree immutably.

### Drop Position Detection

From `onDragEnd` event:
- `event.operation.source` — draggable being moved (has `.data` with our node metadata)
- `event.operation.target` — droppable being hovered (has `.index`, `.group`, `.data`)
- Position inferred from cursor Y offset within target element: top third = before, middle third = inside (groups only), bottom third = after

### Cycle Detection

Before allowing `inside` drop, traverse source subtree. If target ID exists in source descendants, reject. `isDescendant(tree, ancestorId, candidateId)` — pure recursive check.

### Immutable Tree Operations

All tree mutations use spread-based cloning:
- `assignStableIds(node)` — DFS assigns `__id` to every node (for dnd-kit identity)
- `moveNodeInTree(tree, sourceId, targetId, position)` — clone path from root to source removal, then clone path to target insertion point
- `findNodeAndParent(tree, id)` — returns `{ node, parent, index }` or `null`
- `isDescendant(tree, parentId, childId)` — recursive DFS check

## Consequences

- Single `DragDropProvider` wraps both ConditionBuilder and SortBuilder at CompositionBuilder level
- Each sortable item uses `createSortable` with reactive `id`, `index`, `group` getters
- `onChange` callback contract preserved: parent receives full subtree replacement
- No backend changes required
- Keyboard navigation works via built-in `KeyboardSensor`

## Alternatives Considered

| Library | Pros | Cons |
|---------|------|------|
| @dnd-kit/svelte | Already in deps, Svelte 5 native, sortable API | Newer, less community usage |
| sortablejs/svelte-sortable | Battle-tested | Not Svelte 5 compatible, no tree support |
| svelte-dnd-action | Simple API | Svelte 4 only, no hierarchical group concept |
| Custom pointer events | Full control | Reinvent keyboard a11y, collision detection |
