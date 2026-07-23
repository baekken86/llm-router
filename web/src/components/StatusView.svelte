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

  async function toggleDisabled(id, disabled) {
    try {
      await apiFetch(`/api/v1/providers/${id}`, {
        method: 'PUT',
        body: { disabled }
      });
      providers = providers.map(p =>
        p.id === id ? { ...p, disabled } : p
      );
      addToast(disabled ? 'Provider disabled' : 'Provider enabled', 'success');
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function syncModels(id, name) {
    try {
      const result = await apiFetch(`/api/v1/providers/${id}/discover`, { method: 'POST' });
      if (result.deactivated > 0) {
        addToast(`${name}: ${result.deactivated} stale models deactivated`, 'success');
      } else {
        addToast(`${name}: all models up to date`, 'success');
      }
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function updateCostType(id, costType) {
    try {
      await apiFetch(`/api/v1/status/${id}/metadata`, {
        method: 'PUT',
        body: { key: 'cost_type', value: costType }
      });
      providers = providers.map(p =>
        p.id === id ? { ...p, metadata: { ...p.metadata, cost_type: costType } } : p
      );
      addToast('Cost type updated', 'success');
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
        <div class="bg-gray-900 rounded-lg border border-gray-800 px-4 py-3 {p.disabled ? 'opacity-50' : ''}">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-3">
              <span class="text-white font-medium">{p.name}</span>
              {#if p.disabled}
                <span class="inline-flex items-center gap-1.5 text-amber-400 text-sm">
                  <span class="w-2 h-2 rounded-full bg-amber-400"></span>
                  Disabled
                </span>
              {:else if p.rate_limited}
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
              <label class="text-sm text-gray-400">Cost type:</label>
              <select
                class="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
                value={p.metadata?.cost_type || ''}
                onchange={(e) => updateCostType(p.id, e.target.value)}
              >
                <option value="">—</option>
                <option value="free">Free</option>
                <option value="paid">Paid</option>
                <option value="subscription">Subscription</option>
                <option value="api-creds">API Credits</option>
              </select>
              <button
                class="px-2 py-1 text-xs bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
                onclick={() => syncModels(p.id, p.name)}
              >
                Sync
              </button>
              <button
                class="px-2 py-1 text-xs bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
                onclick={() => toggleDisabled(p.id, !p.disabled)}
              >
                {p.disabled ? 'Enable' : 'Disable'}
              </button>
              {#if p.rate_limited && !p.disabled}
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