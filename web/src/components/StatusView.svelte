<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let providers = $state([]);
  let loading = $state(true);

  async function load() {
    loading = true;
    try {
      const data = await apiFetch('/api/v1/status');
      providers = data.providers || [];
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  async function clearRateLimit(id) {
    try {
      await apiFetch(`/api/v1/status/clear/${id}`, { method: 'POST' });
      providers = providers.map(p =>
        p.id === id ? { ...p, rate_limited: false, retry_in: '' } : p
      );
      addToast('Rate limit cleared', 'success');
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  onMount(load);
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-white">Provider Status</h2>
    <button
      class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
      onclick={load}
    >
      Refresh
    </button>
  </div>

  {#if loading}
    <div class="text-gray-400 py-8 text-center">Loading...</div>
  {:else if providers.length === 0}
    <div class="text-gray-400 py-8 text-center">No providers configured</div>
  {:else}
    <div class="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
      <table class="w-full">
        <thead>
          <tr class="border-b border-gray-800 text-left text-sm text-gray-400">
            <th class="px-4 py-3">Provider</th>
            <th class="px-4 py-3">Status</th>
            <th class="px-4 py-3">Details</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          {#each providers as p (p.id)}
            <tr class="border-b border-gray-800/50 hover:bg-gray-800/30">
              <td class="px-4 py-3 text-white font-medium">{p.name}</td>
              <td class="px-4 py-3">
                {#if p.rate_limited}
                  <span class="inline-flex items-center gap-1.5 text-red-400">
                    <span class="w-2 h-2 rounded-full bg-red-400"></span>
                    Rate Limited
                  </span>
                {:else}
                  <span class="inline-flex items-center gap-1.5 text-emerald-400">
                    <span class="w-2 h-2 rounded-full bg-emerald-400"></span>
                    OK
                  </span>
                {/if}
              </td>
              <td class="px-4 py-3 text-gray-400 text-sm">
                {#if p.rate_limited && p.retry_in}
                  Retry in {p.retry_in}
                {:else}
                  &mdash;
                {/if}
              </td>
              <td class="px-4 py-3">
                {#if p.rate_limited}
                  <button
                    class="px-2 py-1 text-xs bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
                    onclick={() => clearRateLimit(p.id)}
                  >
                    Clear
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
