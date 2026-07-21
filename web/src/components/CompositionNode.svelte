<script>
  import ConditionBuilder from './ConditionBuilder.svelte';
  import SortBuilder from './SortBuilder.svelte';
  import { apiFetch } from '../lib/api.js';

  let {
    node,
    depth = 0,
    availableVMs = [],
    isDragTarget = false,
    isChildTarget = false,
    dragSourceId = null,
    dropTargetId = null,
    dropIsOperation = false,
    onDragStart,
    onDragOver,
    onDragLeave,
    onDrop,
    onDragEnd,
    onToggleExpanded,
    onToggleFilter,
    onToggleSort,
    onSetVM,
    onSetOperation,
    onSetFilter,
    onSetSort,
    onRemove,
    onAddFilterSource,
    onAddOperation,
  } = $props();

  const isSource = $derived(!node.operation);
  const isOp = $derived(!!node.operation);

  const isHighlighted = $derived(
    dropTargetId === node.__id && dropIsOperation === isOp
  );
</script>

{#if isSource}
  <!-- Source card (bordered) -->
  <div
    role="listitem"
    class="rounded border transition-colors {isHighlighted ? 'bg-emerald-900/30 border-emerald-500' : 'border-gray-700 hover:border-gray-500'}"
    style="padding-left: {(depth * 20) + 8}px"
  >
    <div
      role="listitem"
      draggable="true"
      ondragstart={(e) => onDragStart(e, node.__id)}
      ondragover={(e) => onDragOver(e, node.__id, false)}
      ondragleave={onDragLeave}
      ondrop={(e) => onDrop(e, node.__id, false)}
      ondragend={onDragEnd}
      class="flex items-center gap-2 px-2 py-1.5 text-sm"
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

      <span class="text-gray-500 text-xs shrink-0">src</span>
      <select
        class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 flex-1 min-w-0 focus:outline-none focus:border-emerald-500"
        value={node.vm ?? ''}
        onchange={(e) => { e.stopPropagation(); onSetVM(node.__id, e.target.value); }}
        onclick={(e) => e.stopPropagation()}
      >
        <option value="">All Models</option>
        {#each availableVMs as vm}
          <option value={vm.name}>{vm.name}{vm.description ? ` — ${vm.description}` : ''}</option>
        {/each}
      </select>
      <button
        class="text-xs bg-gray-700 text-emerald-400 hover:bg-emerald-800 hover:text-emerald-300 px-2 py-1 rounded shrink-0"
        onclick={(e) => { e.stopPropagation(); onToggleFilter(node.__id); }}
        title="Toggle filter"
      >F</button>
      <button
        class="text-xs bg-gray-700 text-blue-400 hover:bg-blue-900 hover:text-blue-300 px-2 py-1 rounded shrink-0"
        onclick={(e) => { e.stopPropagation(); onToggleSort(node.__id); }}
        title="Toggle sort"
      >S</button>
      <button
        class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
        onclick={(e) => { e.stopPropagation(); onRemove(node.__id); }}
        title="Remove source"
      >&times;</button>
    </div>

    {#if node._expandedFilter}
      <div class="px-2 pb-2">
        <div class="bg-gray-900/50 rounded p-2 border border-gray-800">
          <ConditionBuilder
            node={node.filter_expr || { and: [] }}
            onChange={(v) => onSetFilter(node.__id, v)}
          />
        </div>
      </div>
    {/if}
    {#if node._expandedSort}
      <div class="px-2 pb-2">
        <div class="bg-gray-900/50 rounded p-2 border border-gray-800">
          <SortBuilder
            criteria={node.sort_expr || []}
            onChange={(v) => onSetSort(node.__id, v)}
          />
        </div>
      </div>
    {/if}
  </div>

{:else}
  <!-- Operation node -->
  <div
    role="listitem"
    class="rounded border transition-colors {isHighlighted ? 'bg-blue-900/30 border-blue-500 border-dashed' : 'border-transparent hover:border-gray-700'}"
    style="padding-left: {(depth * 20) + 8}px"
    ondragover={(e) => onDragOver(e, node.__id, true)}
    ondragleave={onDragLeave}
    ondrop={(e) => onDrop(e, node.__id, true)}
  >
    <div
      role="listitem"
      draggable="true"
      ondragstart={(e) => onDragStart(e, node.__id)}
      ondragend={onDragEnd}
      class="flex items-center gap-2 px-2 py-1.5 text-sm"
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

      <select
        class="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs font-medium text-gray-200 shrink-0 focus:outline-none focus:border-emerald-500"
        value={node.operation}
        onchange={(e) => { e.stopPropagation(); onSetOperation(node.__id, e.target.value); }}
        onclick={(e) => e.stopPropagation()}
      >
        <option value="union">Union</option>
        <option value="intersection">Intersection</option>
        <option value="difference">Difference</option>
      </select>
      <span class="text-xs text-gray-500 shrink-0">
        {node.sources?.length || 0} sources
      </span>

      <button
        class="text-xs text-gray-500 hover:text-blue-400 px-1 shrink-0"
        onclick={(e) => { e.stopPropagation(); onToggleExpanded(node.__id); }}
        title={node._expanded ? 'Collapse sources' : 'Expand sources'}
      >{node._expanded ? '▾' : '▸'}</button>

      <button
        class="text-xs bg-gray-700 text-red-400 hover:bg-red-900 hover:text-red-300 px-2 py-1 rounded ml-auto shrink-0"
        onclick={(e) => { e.stopPropagation(); onRemove(node.__id); }}
        title="Remove operation"
      >&times;</button>
    </div>

    {#if node._expanded}
      <div class="pl-2 pr-2 pb-2 space-y-1">
        {#if node.sources && node.sources.length > 0}
          {#each node.sources as child (child.__id)}
            <svelte:self
              node={child}
              depth={depth + 1}
              {availableVMs}
              {dragSourceId}
              {dropTargetId}
              {dropIsOperation}
              {onDragStart}
              {onDragOver}
              {onDragLeave}
              {onDrop}
              {onDragEnd}
              {onToggleExpanded}
              {onToggleFilter}
              {onToggleSort}
              {onSetVM}
              {onSetOperation}
              {onSetFilter}
              {onSetSort}
              {onRemove}
              {onAddFilterSource}
              {onAddOperation}
            />
          {/each}
        {/if}
        <div class="flex gap-2 pt-1" style="padding-left: {(depth + 1) * 20 + 8}px">
          <button
            type="button"
            class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
            onclick={(e) => { e.stopPropagation(); onAddFilterSource(node.__id); }}
          >+ Source</button>
          <button
            type="button"
            class="text-xs text-blue-400 hover:text-blue-300 border border-gray-700 rounded px-2 py-1 hover:border-blue-500"
            onclick={(e) => { e.stopPropagation(); onAddOperation(node.__id); }}
          >+ Op</button>
        </div>
      </div>
    {:else}
      <div class="flex gap-2 px-2 pb-2" style="padding-left: {(depth + 1) * 20 + 8}px">
        <button
          type="button"
          class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
          onclick={(e) => { e.stopPropagation(); onAddFilterSource(node.__id); }}
        >+ Source</button>
        <button
          type="button"
          class="text-xs text-blue-400 hover:text-blue-300 border border-gray-700 rounded px-2 py-1 hover:border-blue-500"
          onclick={(e) => { e.stopPropagation(); onAddOperation(node.__id); }}
        >+ Op</button>
      </div>
    {/if}
  </div>
{/if}
