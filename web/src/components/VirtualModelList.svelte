<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let { onCreate, onEdit } = $props();

  function handleLink(e, href) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
  }

  let vms = $state([]);
  let loading = $state(true);
  let expandedId = $state(null);
  let deleteTarget = $state(null);

  async function load() {
    loading = true;
    try {
      vms = await apiFetch('/api/v1/virtual-models');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  async function doDelete() {
    if (!deleteTarget) return;
    try {
      await apiFetch(`/api/v1/virtual-models/${deleteTarget}`, { method: 'DELETE' });
      addToast('Deleted', 'success');
      vms = vms.filter(v => v.id !== deleteTarget);
    } catch (e) {
      addToast(e.message, 'error');
    }
    deleteTarget = null;
  }

  function formatFilter(expr) {
    if (!expr) return 'No filters';
    if (expr.key) {
      const val = Array.isArray(expr.value) ? expr.value.join(',') : expr.value;
      return `${expr.key} ${expr.op} ${val}`;
    }
    if (expr.not) {
      return `NOT (${formatFilter(expr.not)})`;
    }
    const op = expr.and ? 'AND' : 'OR';
    const items = (expr.and || expr.or || []);
    if (items.length === 0) return 'No filters';
    return items.map(i => formatFilter(i)).join(` ${op} `);
  }

  function formatSort(expr) {
    if (!expr || expr.length === 0) return 'No sort';
    return expr.map(c => {
      if (c.condition) {
        const val = Array.isArray(c.condition.value) ? c.condition.value.join(',') : c.condition.value;
        return `IF ${c.condition.key} ${c.condition.op} ${val} ${c.direction || ''}`.trim();
      }
      if (c.direction) return `${c.key} ${c.direction}`;
      if (c.order) return `${c.key}: [${c.order.join(', ')}]`;
      return c.key;
    }).join(', ');
  }

  function formatComposition(node, depth = 0) {
    if (!node) return '';
    if (node.vm) return node.vm;
    if (node.operation) {
      const opSymbol = { union: '∪', intersection: '∩', difference: '\\' }[node.operation] || node.operation;
      const children = (node.sources || []).map(s => formatComposition(s, depth + 1));
      if (depth === 0) return `${node.operation}: ${children.join(' ')} ${opSymbol}`;
      return `(${children.join(` ${opSymbol} `)})`;
    }
    return '?';
  }

  onMount(() => { load(); });
</script>

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Virtual Models</h2>
    <a
      href="/virtual/create"
      class="bg-emerald-600 hover:bg-emerald-700 text-white rounded px-4 py-2 text-sm font-medium no-underline"
      onclick={(e) => { handleLink(e, '/virtual/create'); if (e.defaultPrevented) onCreate(); }}
    >
      + Create New
    </a>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if vms.length === 0}
    <p class="text-gray-500">No virtual models configured.</p>
  {:else}
    <div class="space-y-2">
      {#each vms as vm}
        <div class="bg-gray-900 border border-gray-800 rounded-lg">
          <div
            class="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-gray-850"
            onclick={() => expandedId = expandedId === vm.id ? null : vm.id}
          >
            <div class="flex-1 min-w-0">
              <span class="font-medium text-gray-100">{vm.name}</span>
              {#if vm.description}
                <span class="text-xs text-gray-500 ml-3">{vm.description}</span>
              {/if}
              <div class="text-xs text-gray-500 mt-1">
                {#if vm.composition}
                  <span class="text-blue-400">composite:</span> {formatComposition(vm.composition)}
                {:else}
                  {formatFilter(vm.filter_expr)}
                {/if}
              </div>
            </div>
            <div class="flex gap-2 ml-4">
              <a
                href="/virtual/{vm.id}"
                class="text-xs text-gray-400 hover:text-white px-2 py-1 no-underline"
                onclick={(e) => { e.stopPropagation(); handleLink(e, `/virtual/${vm.id}`); if (e.defaultPrevented) onEdit(vm.id); }}
              >
                Edit
              </a>
              <button
                class="text-xs text-red-400 hover:text-red-300 px-2 py-1"
                onclick={(e) => { e.stopPropagation(); deleteTarget = vm.id; }}
              >
                Delete
              </button>
            </div>
          </div>
          {#if expandedId === vm.id}
            <div class="px-4 pb-3 border-t border-gray-800 pt-3">
              {#if vm.composition}
                <div class="mb-2 text-xs text-gray-500">
                  <span class="text-gray-400">Composition:</span> {formatComposition(vm.composition)}
                </div>
              {:else}
                <div class="mb-2 text-xs text-gray-500">
                  <span class="text-gray-400">Sort:</span> {formatSort(vm.sort_expr)}
                </div>
                <div class="mb-2 text-xs text-gray-500">
                  <span class="text-gray-400">Filter:</span> {formatFilter(vm.filter_expr)}
                </div>
              {/if}
              <div class="mt-3">
                <p class="text-xs text-gray-500 mb-2">Resolved models:</p>
                {#if vm.composition}
                  <ResolvedPreview vmId={vm.id} composition={vm.composition} />
                {:else}
                  <ResolvedPreview vmId={vm.id} filterExpr={vm.filter_expr} sortExpr={vm.sort_expr} />
                {/if}
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<ConfirmDialog
  open={deleteTarget !== null}
  title="Delete Virtual Model"
  message="Are you sure? This cannot be undone."
  onConfirm={doDelete}
  onCancel={() => deleteTarget = null}
/>
