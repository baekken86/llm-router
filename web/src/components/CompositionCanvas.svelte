<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { assignCompositionIds, autoUnwrap, autoWrap, moveNodeBetweenContainers, findNodeInComposition, flattenCompositionIds } from '../lib/treeUtils.js';
  import CompositionBuilder from './CompositionBuilder.svelte';

  let {
    node = $bindable(),
    allVMs = [],
  } = $props();

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

  onMount(() => {
    autoUnwrapInPlace();
    assignCompositionIds(node);
    tree = node;

    // Load available VMs
    if (allVMs.length > 0) {
      availableVMs = allVMs;
    } else {
      apiFetch('/api/v1/virtual-models').then(vms => {
        availableVMs = vms;
      }).catch(() => {});
    }
  });

  function autoUnwrapInPlace() {
    if (!node) return;
    const unwrapped = autoUnwrap(node);
    if (unwrapped !== node) {
      // Replace all keys from node with unwrapped
      for (const key of Object.keys(node)) {
        delete node[key];
      }
      Object.assign(node, unwrapped);
    }
  }

  function addVMRef() {
    tree.sources.push({ vm: '' });
    assignCompositionIds(tree);
    node = tree;
  }

  function addFilterSource() {
    tree.sources.push({ filter_expr: { and: [] }, sort_expr: [] });
    assignCompositionIds(tree);
    node = tree;
  }

  function addOperation() {
    tree.sources.push({ operation: 'union', sources: [{ vm: '' }, { vm: '' }] });
    assignCompositionIds(tree);
    node = tree;
  }

  function removeItem(id) {
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

  /**
   * Expose for parent to call on save.
   * Returns auto-wrapped composition.
   */
  export function getComposition() {
    if (!tree) return null;
    const rootItems = tree.sources || [tree];
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

  function handleCanvasDrop(e) {
    // Handle drop on canvas background (root level)
    if (!dropTargetId && dragSourceId) {
      e.preventDefault();
      const sourceParentId = getParentId(dragSourceId);
      if (sourceParentId) {
        // Move to root level (last position)
        const newTree = moveNodeBetweenContainers(tree, dragSourceId, null, tree.sources.length);
        if (newTree) {
          assignCompositionIds(newTree);
          tree = newTree;
          node = tree;
        }
      }
    }
    dropTargetId = null;
    dragSourceId = null;
  }

  function isVMRef(item) {
    return item.vm !== undefined && !item.operation;
  }

  function isFilterSource(item) {
    return item.filter_expr !== undefined && !item.vm && !item.operation;
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

        {#if isVMRef(item)}
          <!-- VM Reference -->
          <span class="text-gray-500 text-xs shrink-0">ref</span>
          <select
            class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
            value={item.vm || ''}
            onchange={(e) => { e.stopPropagation(); setItemVM(item.__id, e.target.value); }}
            onclick={(e) => e.stopPropagation()}
          >
            <option value="">Select a virtual model...</option>
            {#each availableVMs as vm}
              <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
            {/each}
          </select>

        {:else if isFilterSource(item)}
          <!-- Filter Source -->
          <span class="text-amber-400 text-xs font-medium shrink-0">Filter Source</span>
          <button
            class="text-xs text-gray-500 hover:text-red-400 ml-auto shrink-0"
            onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
            title="Remove filter source"
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
            class="text-xs text-gray-600 hover:text-red-400 ml-auto shrink-0"
            onclick={(e) => { e.stopPropagation(); removeItem(item.__id); }}
            title="Remove operation"
          >&times;</button>
        {/if}
      </div>

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

            {#if isVMRef(source)}
              <span class="text-gray-500 text-xs shrink-0">ref</span>
              <select
                class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
                value={source.vm || ''}
                onchange={(e) => { e.stopPropagation(); setItemVM(source.__id, e.target.value); }}
                onclick={(e) => e.stopPropagation()}
              >
                <option value="">Select a virtual model...</option>
                {#each availableVMs as vm}
                  <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
                {/each}
              </select>

            {:else if isFilterSource(source)}
              <span class="text-amber-400 text-xs font-medium shrink-0">Filter Source</span>

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
        {/each}
      {/if}
    {/each}
  {/if}

  <!-- Root add buttons -->
  <div class="flex gap-2 mt-3" style="padding-left: 8px">
    <button
      class="text-xs text-gray-500 hover:text-emerald-400 border border-gray-700 rounded px-2 py-1 hover:border-emerald-600"
      onclick={addVMRef}
    >+ VM Ref</button>
    <button
      class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
      onclick={addFilterSource}
    >+ Filter Source</button>
    <button
      class="text-xs text-blue-400 hover:text-blue-300 border border-gray-700 rounded px-2 py-1 hover:border-blue-500"
      onclick={addOperation}
    >+ Op</button>
  </div>
</div>
