<script>
  import { onMount, untrack } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { assignCompositionIds, autoUnwrap, autoWrap, moveNodeBetweenContainers, removeNodeFromComposition, findNodeInComposition, flattenCompositionIds } from '../lib/treeUtils.js';
  import CompositionBuilder from './CompositionBuilder.svelte';
  import ConditionBuilder from './ConditionBuilder.svelte';
  import SortBuilder from './SortBuilder.svelte';

  let {
    node = $bindable(),
    allVMs = [],
  } = $props();

  // Bug 3 fix: tree is internal copy, never mutate $bindable prop directly
  let tree = $state(null);
  let dragSourceId = $state(null);
  let dropTargetId = $state(null);
  let availableVMs = $state([]);

  /** Flat list of all __ids for drop-target identification */
  let allIds = $derived(flattenCompositionIds(tree));

  /**
   * Get the parent op node's __id for a given node __id.
   * Returns null if the node is at root level.
   */
  function getParentId(childId) {
    const info = findNodeInComposition(tree, childId);
    return info?.parent?.__id ?? null;
  }

  /**
   * Get depth of a node (0 = root level).
   */
  function getDepth(childId) {
    const info = findNodeInComposition(tree, childId);
    if (!info) return 0;
    let depth = 0;
    let current = info.parent;
    while (current) {
      depth++;
      current = findNodeInComposition(tree, current.__id)?.parent;
    }
    return depth;
  }

  /**
   * Normalize backend response: "collection" → "vm" for internal use.
   */
  function normalizeFromAPI(node) {
    if (!node || typeof node !== 'object') return node;
    if (node.collection !== undefined && node.vm === undefined) {
      node.vm = node.collection;
      delete node.collection;
    }
    if (Array.isArray(node.sources)) {
      node.sources = node.sources.map(normalizeFromAPI);
    }
    return node;
  }

  /**
   * Initialize tree from prop — never mutates the bindable prop.
   */
  function initTree(source) {
    if (!source) { tree = null; return; }
    const copy = normalizeFromAPI({ ...source });
    tree = autoUnwrap(copy);
    untrack(() => assignCompositionIds(tree));
  }

  // Bug 3 fix: $effect watches parent-driven changes to node prop
  $effect(() => {
    initTree(node);
  });

  onMount(() => {
    // Load available VMs
    if (allVMs.length > 0) {
      availableVMs = allVMs;
    } else {
      apiFetch('/api/v1/virtual-models').then(vms => {
        availableVMs = vms;
      }).catch(() => {});
    }
  });

  function addFilterSource() {
    if (!tree) {
      tree = { sources: [] };
    } else if (!tree.sources) {
      tree = { sources: [tree] };
    }
    tree.sources.push({ vm: '' });
    assignCompositionIds(tree);
    node = tree;
  }

  function addOperation() {
    if (!tree) {
      tree = { sources: [] };
    } else if (!tree.sources) {
      tree = { sources: [tree] };
    }
    tree.sources.push({ operation: 'union', sources: [{ vm: '' }, { vm: '' }] });
    assignCompositionIds(tree);
    node = tree;
  }

  function removeItem(id) {
    if (tree && !tree.sources) {
      // Single leaf being removed — clear tree
      tree = null;
      node = tree;
      return;
    }
    const newTree = { ...tree };
    newTree.sources = newTree.sources.filter(s => s.__id !== id);
    tree = newTree;
    node = tree;
  }

  function setItemVM(id, vmName) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node.vm = vmName;
      node = tree;
    }
  }

  function setItemOperation(id, op) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node.operation = op;
      if (!info.node.sources || info.node.sources.length < 2) {
        info.node.sources = [{ vm: '' }, { vm: '' }];
        assignCompositionIds(info.node);
      }
      node = tree;
    }
  }

  function toggleItemExpanded(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expanded = !info.node._expanded;
      node = tree;
    }
  }

  function toggleItemFilter(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expandedFilter = !info.node._expandedFilter;
      node = tree;
    }
  }

  function toggleItemSort(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expandedSort = !info.node._expandedSort;
      node = tree;
    }
  }

  function setItemFilter(id, filterExpr) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      if (filterExpr && (filterExpr.and || filterExpr.or || filterExpr.key)) {
        info.node.filter_expr = filterExpr;
      } else {
        delete info.node.filter_expr;
      }
      node = tree;
    }
  }

  function setItemSort(id, sortCriteria) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      if (sortCriteria && sortCriteria.length > 0) {
        info.node.sort_expr = sortCriteria;
      } else {
        delete info.node.sort_expr;
      }
      node = tree;
    }
  }

  /**
   * Strip internal fields + empty filter/sort from composition tree.
   * Normalize "vm" → "collection" for API serialization.
   * Returns null for nodes that aren't recognizable by the backend.
   */
  function cleanNode(node) {
    if (!node || typeof node !== 'object') return null;
    const cleaned = { ...node };
    if (Array.isArray(cleaned.sources)) {
      cleaned.sources = cleaned.sources.map(cleanNode).filter(Boolean);
    }
    delete cleaned.__id;
    delete cleaned._expanded;
    delete cleaned._expandedFilter;
    delete cleaned._expandedSort;
    if (cleaned.vm !== undefined) {
      cleaned.collection = cleaned.vm;
      delete cleaned.vm;
    }
    if (cleaned.filter_expr && typeof cleaned.filter_expr === 'object') {
      const fe = cleaned.filter_expr;
      const hasContent = fe.key || (Array.isArray(fe.and) && fe.and.length > 0) || (Array.isArray(fe.or) && fe.or.length > 0) || fe.not;
      if (!hasContent) delete cleaned.filter_expr;
    }
    if (cleaned.sort_expr && Array.isArray(cleaned.sort_expr) && cleaned.sort_expr.length === 0) {
      delete cleaned.sort_expr;
    }
    if (!cleaned.operation && cleaned.collection === undefined && !cleaned.filter_expr && !(Array.isArray(cleaned.sort_expr) && cleaned.sort_expr.length > 0)) return null;
    return cleaned;
  }

  /**
   * Expose for parent to call on save.
   * Returns auto-wrapped composition (cleaned, no internal fields).
   */
  export function getComposition() {
    if (!tree) return null;
    const cleaned = cleanNode(tree);
    if (!cleaned) return null;
    const rootItems = cleaned.sources || [cleaned];
    return autoWrap(rootItems);
  }

  // ─── Native HTML5 Drag & Drop ──────────────────────────────────────────

  function handleDragStart(e, id) {
    dragSourceId = id;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', id);
  }

  function handleDragOver(e, id) {
    if (id === dragSourceId) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    dropTargetId = id;
  }

  function handleDragLeave() {
    dropTargetId = null;
  }

  function handleDrop(e, targetId) {
    e.preventDefault();
    dropTargetId = null;

    const sourceId = dragSourceId;
    dragSourceId = null;

    if (!sourceId || sourceId === targetId) return;

    const sourceParentId = getParentId(sourceId);
    const targetParentId = getParentId(targetId);

    if (sourceParentId === null && targetParentId === null) {
      // Both at root: local reorder
      const sources = [...tree.sources];
      const fromIdx = sources.findIndex(s => s.__id === sourceId);
      const toIdx = sources.findIndex(s => s.__id === targetId);
      if (fromIdx === -1 || toIdx === -1) return;
      const [moved] = sources.splice(fromIdx, 1);
      sources.splice(toIdx, 0, moved);
      tree = { ...tree, sources };
      node = tree;
    } else if (sourceParentId !== targetParentId) {
      // Cross-container move
      const targetIdx = tree.sources.findIndex(s => s.__id === targetId);
      const newTree = moveNodeBetweenContainers(tree, sourceId, targetParentId, targetIdx >= 0 ? targetIdx : 0);
      if (newTree) {
        assignCompositionIds(newTree);
        tree = newTree;
        node = tree;
      }
    }
  }

  function handleDragEnd() {
    dragSourceId = null;
    dropTargetId = null;
  }

  function handleCanvasDragOver(e) {
    // Allow dropping on the canvas background (for root-level drops)
    if (!dropTargetId) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    }
  }

  // Bug 1 fix: handle root-level drop directly instead of going through
  // moveNodeBetweenContainers (which returns null for targetParentId === null)
  function handleCanvasDrop(e) {
    // Handle drop on canvas background (root level)
    if (!dropTargetId && dragSourceId) {
      e.preventDefault();
      const sourceParentId = getParentId(dragSourceId);
      if (sourceParentId) {
        // Remove from parent container, append to root
        const sourceInfo = findNodeInComposition(tree, dragSourceId);
        const sourceNode = sourceInfo.node;
        const newTree = removeNodeFromComposition(tree, dragSourceId);
        if (newTree) {
          newTree.sources.push(sourceNode);
          assignCompositionIds(newTree);
          tree = newTree;
          node = tree;
        }
      }
    }
    dropTargetId = null;
    dragSourceId = null;
  }

  function isSource(item) {
    return !item.operation;
  }

  function isOpNode(item) {
    return !!item.operation;
  }
</script>

<div class="space-y-1"
  role="list"
  ondragover={handleCanvasDragOver}
  ondrop={handleCanvasDrop}
>
  {#if tree?.sources}
    {#each tree.sources as item (item.__id)}
      {@const depth = getDepth(item.__id)}
      {@const isDragTarget = dropTargetId === item.__id}

      <div
        role="listitem"
        draggable="true"
        ondragstart={(e) => handleDragStart(e, item.__id)}
        ondragover={(e) => handleDragOver(e, item.__id)}
        ondragleave={handleDragLeave}
        ondrop={(e) => handleDrop(e, item.__id)}
        ondragend={handleDragEnd}
        class="flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors
          {isDragTarget ? 'bg-emerald-900/30 border border-emerald-600' : 'hover:bg-gray-800 border border-transparent'}"
        style="padding-left: {(depth * 20) + 8}px"
      >
        <button
          class="cursor-grab active:cursor-grabbing text-gray-500 hover:text-gray-300 select-none p-0.5 shrink-0"
          aria-label="Drag to reorder"
          title="Drag to reorder"
          tabindex="-1"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="9" cy="6" r="1" /><circle cx="15" cy="6" r="1" />
            <circle cx="9" cy="12" r="1" /><circle cx="15" cy="12" r="1" />
            <circle cx="9" cy="18" r="1" /><circle cx="15" cy="18" r="1" />
          </svg>
        </button>

        {#if isSource(item)}
          <span class="text-gray-500 text-xs shrink-0">src</span>
          <select
            class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
            value={item.vm ?? ''}
            onchange={(e) => { e.stopPropagation(); setItemVM(item.__id, e.target.value); }}
            onclick={(e) => e.stopPropagation()}
          >
            <option value="">All Models</option>
            {#each availableVMs as vm}
              <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
            {/each}
          </select>
          <button
            class="text-xs bg-gray-700 text-emerald-400 hover:bg-emerald-800 hover:text-emerald-300 px-2 py-1 rounded shrink-0"
            onclick={(e) => { e.stopPropagation(); toggleItemFilter(item.__id); }}
            title="Toggle filter"
          >F</button>
          <button
            class="text-xs bg-gray-700 text-blue-400 hover:bg-blue-900 hover:text-blue-300 px-2 py-1 rounded shrink-0"
            onclick={(e) => { e.stopPropagation(); toggleItemSort(item.__id); }}
            title="Toggle sort"
          >S</button>
          <button
            class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
            onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
            title="Remove source"
          >&times;</button>

        {:else if isOpNode(item)}
          <!-- Operation Node -->
          <select
            class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs font-medium text-gray-200 shrink-0 focus:outline-none focus:border-emerald-500"
            value={item.operation}
            onchange={(e) => { e.stopPropagation(); setItemOperation(item.__id, e.target.value); }}
            onclick={(e) => e.stopPropagation()}
          >
            <option value="union">Union</option>
            <option value="intersection">Intersection</option>
            <option value="difference">Difference</option>
          </select>
          <span class="text-xs text-gray-500 shrink-0">
            {item.sources?.length || 0} sources
          </span>

          {#if item.sources && item.sources.length > 0}
            <button
              class="text-xs text-gray-500 hover:text-blue-400 px-1 shrink-0"
              onclick={(e) => { e.stopPropagation(); toggleItemExpanded(item.__id); }}
              title={item._expanded ? 'Collapse sources' : 'Expand sources'}
            >{item._expanded ? '▾' : '▸'}</button>
          {/if}

          <button
            class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
            onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
            title="Remove operation"
          >&times;</button>
        {/if}
      </div>

      {#if isSource(item) && item._expandedFilter}
        <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
          <ConditionBuilder
            node={item.filter_expr || { and: [] }}
            onChange={(v) => setItemFilter(item.__id, v)}
          />
        </div>
      {/if}
      {#if isSource(item) && item._expandedSort}
        <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
          <SortBuilder
            criteria={item.sort_expr || []}
            onChange={(v) => setItemSort(item.__id, v)}
          />
        </div>
      {/if}

      {#if isOpNode(item) && item._expanded && item.sources}
        {#each item.sources as source, idx (source.__id || idx)}
          {@const childDepth = depth + 1}
          {@const isChildDragTarget = dropTargetId === source.__id}
          <div
            role="listitem"
            draggable="true"
            ondragstart={(e) => handleDragStart(e, source.__id)}
            ondragover={(e) => handleDragOver(e, source.__id)}
            ondragleave={handleDragLeave}
            ondrop={(e) => handleDrop(e, source.__id)}
            ondragend={handleDragEnd}
            class="flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors
              {isChildDragTarget ? 'bg-emerald-900/30 border border-emerald-600' : 'hover:bg-gray-800 border border-transparent'}"
            style="padding-left: {(childDepth * 20) + 8}px"
          >
            <button
              class="cursor-grab active:cursor-grabbing text-gray-500 hover:text-gray-300 select-none p-0.5 shrink-0"
              aria-label="Drag to reorder"
              title="Drag to reorder"
              tabindex="-1"
            >
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="9" cy="6" r="1" /><circle cx="15" cy="6" r="1" />
                <circle cx="9" cy="12" r="1" /><circle cx="15" cy="12" r="1" />
                <circle cx="9" cy="18" r="1" /><circle cx="15" cy="18" r="1" />
              </svg>
            </button>

            {#if isSource(source)}
              <span class="text-gray-500 text-xs shrink-0">src</span>
              <select
                class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
                value={source.vm ?? ''}
                onchange={(e) => { e.stopPropagation(); setItemVM(source.__id, e.target.value); }}
                onclick={(e) => e.stopPropagation()}
              >
                <option value="">All Models</option>
                {#each availableVMs as vm}
                  <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
                {/each}
              </select>
              <button class="text-xs bg-gray-700 text-emerald-400 hover:bg-emerald-800 hover:text-emerald-300 px-2 py-1 rounded shrink-0" onclick={(e) => { e.stopPropagation(); toggleItemFilter(source.__id); }} title="Toggle filter">F</button>
              <button class="text-xs bg-gray-700 text-blue-400 hover:bg-blue-900 hover:text-blue-300 px-2 py-1 rounded shrink-0" onclick={(e) => { e.stopPropagation(); toggleItemSort(source.__id); }} title="Toggle sort">S</button>
              <button class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0" onclick={(e) => { e.stopPropagation(); removeItem(source.__id); }} title="Remove source">&times;</button>

            {:else if isOpNode(source)}
              <select
                class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs font-medium text-gray-200 shrink-0 focus:outline-none focus:border-emerald-500"
                value={source.operation}
                onchange={(e) => { e.stopPropagation(); setItemOperation(source.__id, e.target.value); }}
                onclick={(e) => e.stopPropagation()}
              >
                <option value="union">Union</option>
                <option value="intersection">Intersection</option>
                <option value="difference">Difference</option>
              </select>
              <span class="text-xs text-gray-500 shrink-0">
                {source.sources?.length || 0} sources
              </span>
            {/if}
          </div>

          {#if isSource(source) && source._expandedFilter}
            <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
              <ConditionBuilder node={source.filter_expr || { and: [] }} onChange={(v) => setItemFilter(source.__id, v)} />
            </div>
          {/if}
          {#if isSource(source) && source._expandedSort}
            <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
              <SortBuilder criteria={source.sort_expr || []} onChange={(v) => setItemSort(source.__id, v)} />
            </div>
          {/if}

        {/each}
      {/if}
    {/each}
  {:else if tree}
    <!-- Bug 2 fix: single leaf node (no .sources) — render as single root item -->
    {@const item = tree}
    {@const isDragTarget = dropTargetId === item.__id}

    <div
      role="listitem"
      draggable="true"
      ondragstart={(e) => handleDragStart(e, item.__id)}
      ondragover={(e) => handleDragOver(e, item.__id)}
      ondragleave={handleDragLeave}
      ondrop={(e) => handleDrop(e, item.__id)}
      ondragend={handleDragEnd}
      class="flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors
        {isDragTarget ? 'bg-emerald-900/30 border border-emerald-600' : 'hover:bg-gray-800 border border-transparent'}"
      style="padding-left: 8px"
    >
      <button
        class="cursor-grab active:cursor-grabbing text-gray-500 hover:text-gray-300 select-none p-0.5 shrink-0"
        aria-label="Drag to reorder"
        title="Drag to reorder"
        tabindex="-1"
      >
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="9" cy="6" r="1" /><circle cx="15" cy="6" r="1" />
          <circle cx="9" cy="12" r="1" /><circle cx="15" cy="12" r="1" />
          <circle cx="9" cy="18" r="1" /><circle cx="15" cy="18" r="1" />
        </svg>
      </button>

      {#if isSource(item)}
        <span class="text-gray-500 text-xs shrink-0">src</span>
        <select
          class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
          value={item.vm ?? ''}
          onchange={(e) => { e.stopPropagation(); setItemVM(item.__id, e.target.value); }}
          onclick={(e) => e.stopPropagation()}
        >
          <option value="">All Models</option>
          {#each availableVMs as vm}
            <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
          {/each}
        </select>
        <button
          class="text-xs bg-gray-700 text-emerald-400 hover:bg-emerald-800 hover:text-emerald-300 px-2 py-1 rounded shrink-0"
          onclick={(e) => { e.stopPropagation(); toggleItemFilter(item.__id); }}
          title="Toggle filter"
        >F</button>
        <button
          class="text-xs bg-gray-700 text-blue-400 hover:bg-blue-900 hover:text-blue-300 px-2 py-1 rounded shrink-0"
          onclick={(e) => { e.stopPropagation(); toggleItemSort(item.__id); }}
          title="Toggle sort"
        >S</button>
        <button
          class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
          onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
          title="Remove source"
        >&times;</button>

      {:else if isOpNode(item)}
        <select
          class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs font-medium text-gray-200 shrink-0 focus:outline-none focus:border-emerald-500"
          value={item.operation}
          onchange={(e) => { e.stopPropagation(); setItemOperation(item.__id, e.target.value); }}
          onclick={(e) => e.stopPropagation()}
        >
          <option value="union">Union</option>
          <option value="intersection">Intersection</option>
          <option value="difference">Difference</option>
        </select>
        <span class="text-xs text-gray-500 shrink-0">
          {item.sources?.length || 0} sources
        </span>
        <button
          class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
          onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
          title="Remove operation"
        >&times;</button>
      {/if}
    </div>

    {#if isSource(item) && item._expandedFilter}
      <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
        <ConditionBuilder
          node={item.filter_expr || { and: [] }}
          onChange={(v) => setItemFilter(item.__id, v)}
        />
      </div>
    {/if}
    {#if isSource(item) && item._expandedSort}
      <div class="ml-4 mt-1 bg-gray-900/50 rounded p-2 border border-gray-800">
        <SortBuilder
          criteria={item.sort_expr || []}
          onChange={(v) => setItemSort(item.__id, v)}
        />
      </div>
    {/if}
  {/if}

  <!-- Root add buttons -->
  <div class="flex gap-2 mt-3" style="padding-left: 8px">
    <button
      type="button"
      class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
      onclick={addFilterSource}
    >+ Source</button>
    <button
      type="button"
      class="text-xs text-blue-400 hover:text-blue-300 border border-gray-700 rounded px-2 py-1 hover:border-blue-500"
      onclick={addOperation}
    >+ Op</button>
  </div>
</div>
