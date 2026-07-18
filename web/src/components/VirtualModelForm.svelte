<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ConditionBuilder from './ConditionBuilder.svelte';
  import SortBuilder from './SortBuilder.svelte';
  import CompositionBuilder from './CompositionBuilder.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let { vmId = null, onBack } = $props();

  let name = $state('');
  let description = $state('');
  let mode = $state('leaf'); // 'leaf' or 'composite'
  let filterNode = $state({ and: [] });
  let sortCriteria = $state([]);
  let compositionNode = $state({ vm: '' });
  let includeModels = $state([]);
  let maxRetries = $state(0);
  let retryOnStatus = $state([429, 500, 502, 503]);
  let saving = $state(false);
  let loading = $state(vmId !== null);

  const statusOptions = [429, 500, 502, 503, 504];

  async function loadVM() {
    if (!vmId) return;
    try {
      const vm = await apiFetch(`/api/v1/virtual-models/${vmId}`);
      name = vm.name;
      description = vm.description || '';
      maxRetries = vm.max_retries || 0;
      retryOnStatus = vm.retry_on_status || [429, 500, 502, 503];

      if (vm.composition) {
        mode = 'composite';
        compositionNode = vm.composition;
      } else {
        mode = 'leaf';
        if (vm.filter_expr && (vm.filter_expr.and || vm.filter_expr.or)) {
          filterNode = vm.filter_expr;
        }
        if (vm.sort_expr && Array.isArray(vm.sort_expr)) {
          sortCriteria = vm.sort_expr;
        }
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

  function isEmptyNode(n) {
    if (!n) return true;
    if (n.key) return false;
    if (n.and) return n.and.length === 0 || n.and.every(isEmptyNode);
    if (n.or) return n.or.length === 0 || n.or.every(isEmptyNode);
    if (n.not) return isEmptyNode(n.not);
    return true;
  }

  function buildFilterExpr() {
    if (isEmptyNode(filterNode)) return {};
    return filterNode;
  }

  function buildSortExpr() {
    return sortCriteria.filter(c => c.key || c.condition);
  }

  function isValidComposition(node) {
    if (!node) return false;
    if (node.vm) return node.vm !== '';
    if (node.operation) {
      return node.sources && node.sources.length >= 2 && node.sources.every(isValidComposition);
    }
    return false;
  }

  const memoizedFilterExpr = $derived(buildFilterExpr());
  const memoizedSortExpr = $derived(buildSortExpr());

  async function handleSubmit() {
    if (!name.trim()) {
      addToast('Name is required', 'error');
      return;
    }
    if (mode === 'composite' && !isValidComposition(compositionNode)) {
      addToast('Composition must have at least 2 sources with VMs selected', 'error');
      return;
    }
    saving = true;
    try {
      const body = {
        name: name.trim(),
        description: description.trim(),
        max_retries: maxRetries,
        retry_on_status: retryOnStatus,
        include_models: includeModels
      };

      if (mode === 'composite') {
        body.composition = compositionNode;
      } else {
        body.filter_expr = buildFilterExpr();
        body.sort_expr = buildSortExpr();
      }

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

      <!-- Mode toggle -->
      <div>
        <label class="block text-sm text-gray-400 mb-2">Mode</label>
        <div class="flex gap-2">
          <button
            type="button"
            class="px-4 py-2 text-sm rounded border {mode === 'leaf' ? 'bg-emerald-600 border-emerald-600 text-white' : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-600'}"
            onclick={() => mode = 'leaf'}
          >Leaf (Filter/Sort)</button>
          <button
            type="button"
            class="px-4 py-2 text-sm rounded border {mode === 'composite' ? 'bg-emerald-600 border-emerald-600 text-white' : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-600'}"
            onclick={() => mode = 'composite'}
          >Composite (Combine VMs)</button>
        </div>
      </div>

      {#if mode === 'leaf'}
        <div>
          <label class="block text-sm text-gray-400 mb-2">Filter Conditions</label>
          <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
            <ConditionBuilder node={filterNode} onChange={(v) => filterNode = v || { and: [] }} />
          </div>
        </div>

        <div>
          <label class="block text-sm text-gray-400 mb-2">Sort Criteria</label>
          <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
            <SortBuilder criteria={sortCriteria} onChange={(v) => sortCriteria = v} />
          </div>
        </div>
      {:else}
        <div>
          <label class="block text-sm text-gray-400 mb-2">Composition</label>
          <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
            <CompositionBuilder bind:node={compositionNode} />
          </div>
        </div>
      {/if}

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
      {#if mode === 'leaf'}
        <ResolvedPreview previewMode={true} filterExpr={memoizedFilterExpr} sortExpr={memoizedSortExpr} />
      {:else}
        <ResolvedPreview previewMode={true} composition={compositionNode} />
      {/if}
    </div>
  {/if}
</div>
