<script>
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ConditionBuilder from './ConditionBuilder.svelte';
  import SortBuilder from './SortBuilder.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let { vmId = null, onBack } = $props();

  let name = $state('');
  let conditions = $state([]);
  let sortCriteria = $state([]);
  let maxRetries = $state(0);
  let retryOnStatus = $state([429, 500, 502, 503]);
  let savedId = $state(null);
  let saving = $state(false);
  let loading = $state(vmId !== null);

  const statusOptions = [429, 500, 502, 503, 504];

  async function loadVM() {
    if (!vmId) return;
    try {
      const vm = await apiFetch(`/api/v1/virtual-models/${vmId}`);
      name = vm.name;
      maxRetries = vm.max_retries || 0;
      retryOnStatus = vm.retry_on_status || [429, 500, 502, 503];

      if (vm.filter_expr && vm.filter_expr.and) {
        conditions = vm.filter_expr.and;
      }
      if (vm.sort_expr && Array.isArray(vm.sort_expr)) {
        sortCriteria = vm.sort_expr;
      }
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function buildFilterExpr() {
    const valid = conditions.filter(c => c.key && c.op);
    if (valid.length === 0) return {};
    return { and: valid };
  }

  function buildSortExpr() {
    return sortCriteria.filter(c => c.key);
  }

  async function handleSubmit() {
    if (!name.trim()) {
      addToast('Name is required', 'error');
      return;
    }
    saving = true;
    try {
      const body = {
        name: name.trim(),
        filter_expr: buildFilterExpr(),
        sort_expr: buildSortExpr(),
        max_retries: maxRetries,
        retry_on_status: retryOnStatus
      };

      let result;
      if (vmId) {
        result = await apiFetch(`/api/v1/virtual-models/${vmId}`, {
          method: 'PUT',
          body
        });
      } else {
        result = await apiFetch('/api/v1/virtual-models', {
          method: 'POST',
          body
        });
      }
      savedId = result.id;
      addToast(vmId ? 'Updated' : 'Created', 'success');
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

  $effect(() => { loadVM(); });
</script>

<div>
  <button class="text-sm text-gray-400 hover:text-white mb-4" onclick={onBack}>
    &larr; Back to list
  </button>

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
        <label class="block text-sm text-gray-400 mb-2">Filter Conditions</label>
        <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <ConditionBuilder {conditions} onchange={(v) => conditions = v} />
        </div>
      </div>

      <div>
        <label class="block text-sm text-gray-400 mb-2">Sort Criteria</label>
        <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <SortBuilder criteria={sortCriteria} onchange={(v) => sortCriteria = v} />
        </div>
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

      <div class="flex gap-3">
        <button
          type="submit"
          disabled={saving}
          class="bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white rounded px-6 py-2 text-sm font-medium"
        >
          {saving ? 'Saving...' : vmId ? 'Update' : 'Create'}
        </button>
        <button
          type="button"
          class="text-sm text-gray-400 hover:text-white px-4 py-2"
          onclick={onBack}
        >
          Cancel
        </button>
      </div>
    </form>

    {#if savedId}
      <div class="mt-8 bg-gray-900 border border-gray-800 rounded-lg p-4">
        <h3 class="text-sm font-medium text-gray-300 mb-3">Resolved Models</h3>
        <ResolvedPreview vmId={savedId} filterExpr={buildFilterExpr()} sortExpr={buildSortExpr()} />
      </div>
    {/if}
  {/if}
</div>
