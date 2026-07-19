<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let mappings = $state([]);
  let loading = $state(true);
  let filter = $state('');
  let deleteConfirmId = $state(null);

  const filtered = $derived.by(() => {
    if (!filter) return mappings;
    const q = filter.toLowerCase();
    return mappings.filter(m =>
      m.source_model_name.toLowerCase().includes(q) ||
      m.target_model_name.toLowerCase().includes(q)
    );
  });

  async function load() {
    loading = true;
    try {
      mappings = await apiFetch('/api/v1/mappings');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  async function deleteMapping(sourceId) {
    try {
      await apiFetch(`/api/v1/models/${sourceId}/mapping`, { method: 'DELETE' });
      addToast('Mapping deleted', 'success');
      deleteConfirmId = null;
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  onMount(() => { load(); });
</script>

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Mappings</h2>
    <div class="flex items-center gap-4">
      <input
        type="text"
        placeholder="Filter by name..."
        bind:value={filter}
        class="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-emerald-500"
      />
      <span class="text-sm text-gray-500">{filtered.length} mappings</span>
    </div>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if filtered.length === 0}
    <p class="text-gray-500">No mappings configured. Map models from the Raw Models tab.</p>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-gray-500 border-b border-gray-800">
            <th class="text-left px-4 py-3">Source Model</th>
            <th class="text-left px-4 py-3">Source Provider</th>
            <th class="text-center px-4 py-3"></th>
            <th class="text-left px-4 py-3">Target Model</th>
            <th class="text-left px-4 py-3">Target Provider</th>
            <th class="text-left px-4 py-3">Created</th>
            <th class="text-right px-4 py-3">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as m}
            <tr class="border-b border-gray-850 hover:bg-gray-850/50">
              <td class="px-4 py-2.5 text-emerald-400 font-mono">{m.source_model_name}</td>
              <td class="px-4 py-2.5 text-gray-300">{m.source_provider_name}</td>
              <td class="px-4 py-2.5 text-center text-gray-500">→</td>
              <td class="px-4 py-2.5 text-blue-400 font-mono">{m.target_model_name}</td>
              <td class="px-4 py-2.5 text-gray-300">{m.target_provider_name}</td>
              <td class="px-4 py-2.5 text-gray-500 text-xs">{m.created_at}</td>
              <td class="px-4 py-2.5 text-right">
                {#if deleteConfirmId === m.source_model_id}
                  <span class="text-xs text-gray-400 mr-2">Delete?</span>
                  <button
                    class="text-xs text-red-400 hover:text-red-300 mr-2"
                    onclick={() => deleteMapping(m.source_model_id)}
                  >Yes</button>
                  <button
                    class="text-xs text-gray-500 hover:text-gray-300"
                    onclick={() => deleteConfirmId = null}
                  >No</button>
                {:else}
                  <button
                    class="text-xs text-gray-500 hover:text-red-400"
                    onclick={() => deleteConfirmId = m.source_model_id}
                  >Delete</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
