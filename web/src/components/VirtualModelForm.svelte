<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast, sortConditions, filterConditions } from '../lib/stores.js';
  import CompositionCanvas from './CompositionCanvas.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let { vmId = null, onBack } = $props();

  let name = $state('');
  let description = $state('');
  let compositionNode = $state(null);
  let includeModels = $state([]);
  let maxRetries = $state(0);
  let retryOnStatus = $state([429, 500, 502, 503]);
  let disabledGlobalConditions = $state([]);
  let globalConditions = $state([]);
  let disabledGlobalFilterConditions = $state([]);
  let globalFilterConditions = $state([]);
  let saving = $state(false);
  let loading = $state(vmId !== null);
  let canvasRef = $state();

  const statusOptions = [429, 500, 502, 503, 504];

  async function loadVM() {
    if (!vmId) return;
    try {
      const [vm, gsc, gfc] = await Promise.all([
        apiFetch(`/api/v1/virtual-models/${vmId}`),
        apiFetch('/api/v1/global-sort-conditions').catch(() => []),
        apiFetch('/api/v1/global-filter-conditions').catch(() => []),
      ]);
      globalConditions = Array.isArray(gsc) ? gsc : [];
      sortConditions.set(globalConditions);
      globalFilterConditions = Array.isArray(gfc) ? gfc : [];
      filterConditions.set(globalFilterConditions);
      disabledGlobalConditions = Array.isArray(vm.disabled_global_sort_conditions)
        ? vm.disabled_global_sort_conditions
        : [];
      disabledGlobalFilterConditions = Array.isArray(vm.disabled_global_filter_conditions)
        ? vm.disabled_global_filter_conditions
        : [];
      name = vm.name;
      description = vm.description || '';
      maxRetries = vm.max_retries || 0;
      retryOnStatus = vm.retry_on_status || [429, 500, 502, 503];

      if (vm.composition) {
        compositionNode = vm.composition;
      } else if (vm.filter_expr && (vm.filter_expr.and || vm.filter_expr.or || vm.filter_expr.key)) {
        // Auto-convert leaf VM to composition tree on edit
        compositionNode = {
          filter_expr: vm.filter_expr,
          sort_expr: vm.sort_expr || []
        };
      }
      if (vm.include_models && Array.isArray(vm.include_models)) {
        includeModels = vm.include_models;
      }
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function isValidComposition(node) {
    if (!node) return false;
    if (node.operation) {
      return node.sources && node.sources.length >= 2 && node.sources.every(isValidComposition);
    }
    // Source node — always valid (all models default, optional collection/filter/sort)
    return true;
  }

  async function handleSubmit() {
    if (!name.trim()) {
      addToast('Name is required', 'error');
      return;
    }
    if (!isValidComposition(canvasRef?.getComposition() || compositionNode)) {
      addToast('Composition must have at least 2 sources with VMs selected, or a valid filter source', 'error');
      return;
    }
    saving = true;
    try {
      const composition = canvasRef?.getComposition() || compositionNode;
      const body = {
        name: name.trim(),
        description: description.trim(),
        max_retries: maxRetries,
        retry_on_status: retryOnStatus,
        include_models: includeModels,
        disabled_global_sort_conditions: disabledGlobalConditions,
        disabled_global_filter_conditions: disabledGlobalFilterConditions,
        composition
      };

      let result;
      if (vmId) {
        result = await apiFetch(`/api/v1/virtual-models/${vmId}`, {
          method: 'PUT',
          body
        });
        addToast('Updated', 'success');
      } else {
        result = await apiFetch('/api/v1/virtual-models', {
          method: 'POST',
          body
        });
        addToast('Created', 'success');
        if (result?.id) {
          vmId = result.id;
        }
      }
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  function toggleGlobalFilterCondition(id) {
    disabledGlobalFilterConditions = disabledGlobalFilterConditions.includes(id)
      ? disabledGlobalFilterConditions.filter(x => x !== id)
      : [...disabledGlobalFilterConditions, id];
  }

  function toggleGlobalCondition(id) {
    disabledGlobalConditions = disabledGlobalConditions.includes(id)
      ? disabledGlobalConditions.filter(x => x !== id)
      : [...disabledGlobalConditions, id];
  }

  function toggleStatus(code) {
    if (retryOnStatus.includes(code)) {
      retryOnStatus = retryOnStatus.filter(c => c !== code);
    } else {
      retryOnStatus = [...retryOnStatus, code];
    }
  }

  onMount(() => { loadVM(); });
</script>

<div>
  <a href="/virtual" class="text-sm text-gray-400 hover:text-white mb-4 no-underline inline-block" onclick={(e) => { if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; e.preventDefault(); onBack(); }}>
    &larr; Back to list
  </a>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else}
    <h2 class="text-xl font-bold text-gray-100 mb-6">{vmId ? 'Edit' : 'Create'} Virtual Model</h2>

    <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="space-y-6 max-w-3xl">
      <div>
        <label class="block text-sm text-gray-400 mb-1">Name</label>
        <input
          type="text"
          bind:value={name}
          placeholder="e.g. smart-free, docs, planning"
          class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
        />
      </div>

      <div>
        <label class="block text-sm text-gray-400 mb-1">Description</label>
        <textarea
          bind:value={description}
          placeholder="What does this virtual model do? How is it used?"
          rows="2"
          class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
        ></textarea>
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-sm text-gray-400 mb-1">Max Retries</label>
          <input
            type="number"
            bind:value={maxRetries}
            min="0"
            max="3"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
          />
        </div>
        <div>
          <label class="block text-sm text-gray-400 mb-1">Retry on Status</label>
          <div class="flex gap-2 flex-wrap">
            {#each statusOptions as code}
              <button
                type="button"
                class="px-3 py-1 text-xs rounded {retryOnStatus.includes(code) ? 'bg-emerald-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
                onclick={() => toggleStatus(code)}
              >
                {code}
              </button>
            {/each}
          </div>
        </div>
      </div>

      <div>
        <label class="block text-sm text-gray-400 mb-2">Global Filter Conditions</label>
        <p class="text-xs text-gray-500 mb-2">
          Applied first, before model-specific filters. Uncheck to disable for this model.
        </p>
        {#if globalFilterConditions.length === 0}
          <p class="text-xs text-gray-500">No global filter conditions defined.</p>
        {:else}
          <div class="space-y-2">
            {#each globalFilterConditions.filter(c => c.enabled !== false) as c (c.id)}
              <label class="flex items-center gap-3 bg-gray-900 border border-gray-800 rounded px-3 py-2">
                <input
                  type="checkbox"
                  checked={!disabledGlobalFilterConditions.includes(c.id)}
                  onchange={() => toggleGlobalFilterCondition(c.id)}
                  class="accent-emerald-600"
                />
                <span class="text-sm text-gray-200 font-mono">{c.name}</span>
                <span class="text-xs text-gray-500 truncate flex-1">{c.description || ''}</span>
              </label>
            {/each}
            {#each globalFilterConditions.filter(c => c.enabled === false) as c (c.id)}
              <div class="flex items-center gap-3 bg-gray-900 border border-gray-800 rounded px-3 py-2 opacity-50">
                <span class="text-sm text-gray-500 font-mono line-through">{c.name}</span>
                <span class="text-xs text-gray-600">(globally disabled)</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <div>
        <label class="block text-sm text-gray-400 mb-2">Global Sort Conditions</label>
        <p class="text-xs text-gray-500 mb-2">
          Applied before model-specific sorts. Uncheck to disable for this model.
        </p>
        {#if globalConditions.length === 0}
          <p class="text-xs text-gray-500">No global sort conditions defined.</p>
        {:else}
          <div class="space-y-2">
            {#each globalConditions.filter(c => c.enabled !== false) as c (c.id)}
              <label
                class="flex items-center gap-3 bg-gray-900 border border-gray-800 rounded px-3 py-2"
              >
                <input
                  type="checkbox"
                  checked={!disabledGlobalConditions.includes(c.id)}
                  onchange={() => toggleGlobalCondition(c.id)}
                  class="accent-emerald-600"
                />
                <span class="text-sm text-gray-200 font-mono">{c.name}</span>
                <span class="text-xs text-gray-500 truncate flex-1">{c.description || ''}</span>
              </label>
            {/each}
            {#each globalConditions.filter(c => c.enabled === false) as c (c.id)}
              <div class="flex items-center gap-3 bg-gray-900 border border-gray-800 rounded px-3 py-2 opacity-50">
                <span class="text-sm text-gray-500 font-mono line-through">{c.name}</span>
                <span class="text-xs text-gray-600">(globally disabled)</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <div>
        <label class="block text-sm text-gray-400 mb-2">Composition</label>
        <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <CompositionCanvas bind:node={compositionNode} bind:this={canvasRef} />
        </div>
      </div>

      <div class="flex gap-3">
        <button
          type="submit"
          disabled={saving}
          class="bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white rounded px-6 py-2 text-sm font-medium"
        >
          {saving ? 'Saving...' : vmId ? 'Update' : 'Create'}
        </button>
        <a
          href="/virtual"
          type="button"
          class="text-sm text-gray-400 hover:text-white px-4 py-2 no-underline"
          onclick={(e) => { if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; e.preventDefault(); onBack(); }}
        >
          Cancel
        </a>
      </div>
    </form>

    <div class="mt-8 bg-gray-900 border border-gray-800 rounded-lg p-4">
      <h3 class="text-sm font-medium text-gray-300 mb-3">
        Resolved Models <span class="text-gray-500 text-xs">(live preview)</span>
      </h3>
      <ResolvedPreview previewMode={true} composition={canvasRef?.getComposition() || compositionNode} />
    </div>
  {/if}
</div>
