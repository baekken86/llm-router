<script>
  import ConditionBuilder from './ConditionBuilder.svelte';
  import SortBuilder from './SortBuilder.svelte';
  import CompositionBuilder from './CompositionBuilder.svelte';
  import SortableTree from './SortableTree.svelte';
  import SortableItem from './SortableItem.svelte';
  import DragHandle from './DragHandle.svelte';
  import { assignStableIds } from '../lib/treeUtils.js';
  import { apiFetch } from '../lib/api.js';
  import { onMount } from 'svelte';

  let { node = $bindable(), allVMs = [], onChange } = $props();

  let availableVMs = $state([]);
  let loadingVMs = $state(false);
  let expandedFilter = $state(false);
  let expandedSort = $state(false);

  async function loadVMs() {
    if (allVMs.length > 0) {
      availableVMs = allVMs;
      return;
    }
    loadingVMs = true;
    try {
      availableVMs = await apiFetch('/api/v1/virtual-models');
    } catch (e) {
      // silently fail - dropdown will be empty
    } finally {
      loadingVMs = false;
    }
  }

  onMount(() => { loadVMs(); });

  function emit() {
    onChange?.(node);
  }

  function addSource() {
    if (!node.sources) node.sources = [];
    node.sources.push({ vm: '' });
    emit();
  }

  function addNestedOp() {
    if (!node.sources) node.sources = [];
    node.sources.push({ operation: 'union', sources: [{ vm: '' }, { vm: '' }] });
    emit();
  }

  function removeSource(idx) {
    node.sources.splice(idx, 1);
    if (node.sources.length < 2 && node.operation) {
      // Keep at least what's there, but warn visually
    }
    emit();
  }

  function setSourceVM(idx, vmName) {
    node.sources[idx] = { ...node.sources[idx], vm: vmName };
    emit();
  }

  function setOperation(op) {
    node.operation = op;
    if (!node.sources || node.sources.length < 2) {
      node.sources = [{ vm: '' }, { vm: '' }];
    }
    emit();
  }

  function canRemoveSource() {
    return node.sources && node.sources.length > 2;
  }

  function switchToVMRef() {
    delete node.operation;
    delete node.sources;
    delete node.filter_expr;
    delete node.sort_expr;
    node.vm = '';
    emit();
  }

  function switchToOp() {
    delete node.vm;
    node.operation = 'union';
    node.sources = [{ vm: '' }, { vm: '' }];
    emit();
  }

  function switchToFilterSource() {
    delete node.vm;
    delete node.operation;
    delete node.sources;
    node.filter_expr = { and: [] };
    node.sort_expr = [];
    emit();
  }

  function addFilterSource() {
    if (!node.sources) node.sources = [];
    node.sources.push({ filter_expr: { and: [] }, sort_expr: [] });
    emit();
  }

  function updateNodeFilter(filterExpr) {
    if (filterExpr && (filterExpr.and || filterExpr.or || filterExpr.key)) {
      node.filter_expr = filterExpr;
    } else {
      delete node.filter_expr;
    }
    emit();
  }

  function updateNodeSort(sortExpr) {
    if (sortExpr && sortExpr.length > 0) {
      node.sort_expr = sortExpr;
    } else {
      delete node.sort_expr;
    }
    emit();
  }

  function handleSourceChange(idx, childNode) {
    node.sources[idx] = childNode;
    emit();
  }

  function ensureSourceIds() {
    if (!node.sources) return;
    for (const s of node.sources) {
      if (!s.__id) assignStableIds(s);
    }
  }

  function handleDragEnd(event) {
    const { operation } = event;
    const fromIdx = operation.source?.index;
    const toIdx = operation.target?.index;
    if (fromIdx === toIdx || fromIdx == null || toIdx == null) return;
    const next = [...node.sources];
    const [moved] = next.splice(fromIdx, 1);
    next.splice(toIdx, 0, moved);
    node.sources = next;
    emit();
  }

  const opColors = {
    union: 'border-l-blue-500',
    intersection: 'border-l-green-500',
    difference: 'border-l-amber-500',
  };

  const opLabels = {
    union: 'Union',
    intersection: 'Intersection',
    difference: 'Difference',
  };
</script>

{#if node.filter_expr !== undefined && !node.vm && !node.operation}
  <!-- Inline Filter Source node -->
  <div class="border-l-2 border-l-amber-500 pl-3 space-y-2">
    <div class="flex items-center gap-2 mb-2">
      <span class="text-amber-400 text-xs font-medium">Filter Source</span>
      <button
        class="text-xs text-gray-500 hover:text-emerald-400 px-1"
        onclick={() => { expandedFilter = !expandedFilter; }}
        title="Toggle filter"
      >F</button>
      <button
        class="text-xs text-gray-500 hover:text-blue-400 px-1"
        onclick={() => { expandedSort = !expandedSort; }}
        title="Toggle sort"
      >S</button>
      <button
        class="text-xs text-gray-600 hover:text-emerald-400"
        onclick={() => switchToVMRef()}
        title="Convert to VM reference"
      >{String.fromCharCode(0x2192)}ref</button>
      <button
        class="text-xs text-gray-600 hover:text-blue-400"
        onclick={() => switchToOp()}
        title="Convert to operation node"
      >{String.fromCharCode(0x2192)}op</button>
    </div>
    {#if expandedFilter}
      <div class="ml-4 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
        <ConditionBuilder node={node.filter_expr || { and: [] }} onChange={(v) => updateNodeFilter(v)} />
      </div>
    {/if}
    {#if expandedSort}
      <div class="ml-4 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
        <SortBuilder criteria={node.sort_expr || []} onChange={(v) => updateNodeSort(v)} />
      </div>
    {/if}
  </div>

{:else if node.vm !== undefined && !node.operation}
  <!-- VM Reference node -->
  <div class="flex items-center gap-2 bg-gray-800 rounded px-3 py-2 text-sm border border-gray-700">
    <span class="text-gray-500 text-xs">ref</span>
    <select
      class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 focus:outline-none focus:border-emerald-500"
      value={node.vm || ''}
      onchange={(e) => { node.vm = e.target.value; emit(); }}
    >
      <option value="">Select a virtual model...</option>
      {#each availableVMs as vm}
        <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
      {/each}
    </select>
    {#if node.filter_expr || (node.sort_expr && node.sort_expr.length > 0)}
      <span class="text-xs text-emerald-400" title="Has post-filter/sort">filtered</span>
    {/if}
    <button
      class="text-xs text-gray-500 hover:text-emerald-400 px-1"
      onclick={() => { expandedFilter = !expandedFilter; }}
      title="Toggle filter"
    >F</button>
    <button
      class="text-xs text-gray-500 hover:text-blue-400 px-1"
      onclick={() => { expandedSort = !expandedSort; }}
      title="Toggle sort"
    >S</button>
    <button
      class="text-xs text-gray-600 hover:text-red-400"
      onclick={() => switchToOp()}
      title="Convert to operation node"
    >+op</button>
  </div>
  {#if expandedFilter}
    <div class="ml-4 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
      <ConditionBuilder node={node.filter_expr || { and: [] }} onChange={(v) => updateNodeFilter(v)} />
    </div>
  {/if}
  {#if expandedSort}
    <div class="ml-4 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
      <SortBuilder criteria={node.sort_expr || []} onChange={(v) => updateNodeSort(v)} />
    </div>
  {/if}

{:else if node.operation}
  <!-- Operation node -->
  <div class="border-l-2 {opColors[node.operation] || 'border-l-gray-600'} pl-3 space-y-2">
    <div class="flex items-center gap-2 mb-2">
      <select
        class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs font-medium text-gray-200 focus:outline-none focus:border-emerald-500"
        value={node.operation}
        onchange={(e) => setOperation(e.target.value)}
      >
        <option value="union">Union</option>
        <option value="intersection">Intersection</option>
        <option value="difference">Difference</option>
      </select>
      <span class="text-xs text-gray-500">
        {opLabels[node.operation]} of {node.sources?.length || 0} sources
      </span>
      <button
        class="text-xs text-gray-500 hover:text-emerald-400 px-1"
        onclick={() => { expandedFilter = !expandedFilter; }}
        title="Toggle post-operation filter"
      >F</button>
      <button
        class="text-xs text-gray-500 hover:text-blue-400 px-1"
        onclick={() => { expandedSort = !expandedSort; }}
        title="Toggle post-operation sort"
      >S</button>
      <button
        class="text-xs text-gray-600 hover:text-red-400"
        onclick={() => switchToVMRef()}
        title="Convert to VM reference"
      >-op</button>
    </div>

    <!-- Nested sources -->
    {#if node.sources}
      {@const _ = ensureSourceIds()}
      <SortableTree onDragEnd={handleDragEnd}>
        <div class="space-y-2">
          {#each node.sources as source, idx (source.__id || idx)}
            <SortableItem
              id={source.__id || `comp-src-${idx}`}
              index={idx}
              group={`comp-${node.__id || 'root'}`}
              data={{ type: 'comp-source', idx }}
            >
              {#snippet children(sortable)}
                <div class="relative">
                  <div class="absolute left-0 top-0 bottom-0 w-px bg-gray-700"></div>
                  <div class="pl-3 flex items-start gap-1">
                    {#if node.sources.length > 1}
                      <DragHandle attachHandle={sortable.attachHandle} />
                    {/if}
                    <div class="flex-1 min-w-0">
                      <CompositionBuilder node={source} allVMs={availableVMs} onChange={(childNode) => handleSourceChange(idx, childNode)} />
                    </div>
                    <button
                      class="shrink-0 self-center w-4 h-4 rounded-full {canRemoveSource() ? 'bg-gray-700 text-gray-400 hover:bg-red-900 hover:text-red-300' : 'bg-gray-800 text-gray-700 cursor-not-allowed'} text-xs flex items-center justify-center"
                      onclick={() => canRemoveSource() && removeSource(idx)}
                      disabled={!canRemoveSource()}
                      title={canRemoveSource() ? 'Remove source' : 'Minimum 2 sources required'}
                    >&times;</button>
                  </div>
                </div>
              {/snippet}
            </SortableItem>
          {/each}
        </div>
      </SortableTree>
    {/if}

    <!-- Add source buttons -->
    <div class="flex gap-2 ml-3">
      <button
        class="text-xs text-gray-500 hover:text-emerald-400 border border-gray-700 rounded px-2 py-1 hover:border-emerald-600"
        onclick={addSource}
      >+ Source</button>
      <button
        class="text-xs text-gray-500 hover:text-blue-400 border border-gray-700 rounded px-2 py-1 hover:border-blue-600"
        onclick={addNestedOp}
      >+ Nested Op</button>
      <button
        class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
        onclick={addFilterSource}
      >+ Filter Source</button>
    </div>

    {#if expandedFilter}
      <div class="ml-3 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
        <p class="text-xs text-gray-500 mb-1">Post-operation filter:</p>
        <ConditionBuilder node={node.filter_expr || { and: [] }} onChange={(v) => updateNodeFilter(v)} />
      </div>
    {/if}
    {#if expandedSort}
      <div class="ml-3 mt-1 bg-gray-850 rounded p-2 border border-gray-800">
        <p class="text-xs text-gray-500 mb-1">Post-operation sort:</p>
        <SortBuilder criteria={node.sort_expr || []} onChange={(v) => updateNodeSort(v)} />
      </div>
    {/if}
  </div>
{/if}
