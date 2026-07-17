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
    <div class="space-y-3">
      {#each providers as p (p.id)}
        <div class="bg-gray-900 rounded-lg border border-gray-800 px-4 py-3">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-3">
              <span class="text-white font-medium">{p.name}</span>
              {#if p.rate_limited}
                <span class="inline-flex items-center gap-1.5 text-red-400 text-sm">
                  <span class="w-2 h-2 rounded-full bg-red-400"></span>
                  Rate Limited
                  {#if p.retry_in}
                    <span class="text-gray-500">({p.retry_in})</span>
                  {/if}
                </span>
              {:else}
                <span class="inline-flex items-center gap-1.5 text-emerald-400 text-sm">
                  <span class="w-2 h-2 rounded-full bg-emerald-400"></span>
                  OK
                </span>
              {/if}
            </div>
            <div class="flex items-center gap-2">
              {#if p.rate_limited}
                <button
                  class="px-2 py-1 text-xs bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
                  onclick={() => clearRateLimit(p.id)}
                >
                  Clear
                </button>
              {/if}
            </div>
          </div>
          <div class="mt-2 flex items-center gap-4 text-sm text-gray-400">
            {#if p.oauth_configured}
              <span class="flex items-center gap-1.5">
                {#if p.oauth_expired}
                  <span class="text-red-400">OAuth EXPIRED</span>
                {:else}
                  <span class="text-emerald-400">OAuth</span>
                  {#if p.oauth_email}
                    <span>({p.oauth_email})</span>
                  {/if}
                {/if}
              </span>
              {#if p.oauth_expires_at}
                <span class="text-gray-500">expires: {new Date(p.oauth_expires_at).toLocaleString()}</span>
              {/if}
            {:else if p.api_key_configured}
              <span class="text-emerald-400">API key configured</span>
            {:else}
              <span class="text-red-400">No credentials</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
