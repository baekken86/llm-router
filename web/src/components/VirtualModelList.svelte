<script>
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let { onCreate, onEdit } = $props();

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
    if (!expr || !expr.and || expr.and.length === 0) return 'No filters';
    return expr.and.map(c => `${c.key} ${c.op} ${Array.isArray(c.value) ? c.value.join(',') : c.value}`).join(' AND ');
  }

  function formatSort(expr) {
    if (!expr || expr.length === 0) return 'No sort';
    return expr.map(c => {
      if (c.direction) return `${c.key} ${c.direction}`;
      if (c.order) return `${c.key}: [${c.order.join(', ')}]`;
      return c.key;
    }).join(', ');
  }

  $effect(() => { load(); });
</script>

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Virtual Models</h2>
    <button
      class="bg-emerald-600 hover:bg-emerald-700 text-white rounded px-4 py-2 text-sm font-medium"
      onclick={onCreate}
    >
      + Create New
    </button>
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
              <span class="text-xs text-gray-500 ml-3">{formatFilter(vm.filter_expr)}</span>
            </div>
            <div class="flex gap-2 ml-4">
              <button
                class="text-xs text-gray-400 hover:text-white px-2 py-1"
                onclick={(e) => { e.stopPropagation(); onEdit(vm.id); }}
              >
                Edit
              </button>
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
              <div class="mb-2 text-xs text-gray-500">
                <span class="text-gray-400">Sort:</span> {formatSort(vm.sort_expr)}
              </div>
              <div class="mb-2 text-xs text-gray-500">
                <span class="text-gray-400">Filter:</span> {formatFilter(vm.filter_expr)}
              </div>
              <div class="mt-3">
                <p class="text-xs text-gray-500 mb-2">Resolved models:</p>
                <ResolvedPreview vmId={vm.id} filterExpr={vm.filter_expr} sortExpr={vm.sort_expr} />
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
