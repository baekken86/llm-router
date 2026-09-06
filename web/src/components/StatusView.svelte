<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let providers = $state([]);
  let loading = $state(true);
  let showDisableModal = $state(false);
  let disableTarget = $state(null);
  let disableDuration = $state('10m');
  let now = $state(Date.now());

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

  async function toggleDisabled(id, disabled, duration) {
    try {
      const body = { disabled };
      if (disabled && duration) body.duration = duration;
      await apiFetch(`/api/v1/providers/${id}`, {
        method: 'PUT',
        body
      });
      providers = providers.map(p => {
        if (p.id === id) {
          const updated = { ...p, disabled };
          if (disabled && duration) {
            const ms = { '10m': 600000, '1h': 3600000, '24h': 86400000 }[duration];
            updated.disabled_until = new Date(Date.now() + ms).toISOString();
          } else if (!disabled) {
            updated.disabled_until = null;
          }
          return updated;
        }
        return p;
      });
      addToast(disabled ? 'Provider disabled' : 'Provider enabled', 'success');
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  function openDisableModal(provider) {
    disableTarget = provider;
    disableDuration = '10m';
    showDisableModal = true;
  }

  async function confirmDisable() {
    if (!disableTarget) return;
    const dur = disableDuration === 'indefinite' ? null : disableDuration;
    await toggleDisabled(disableTarget.id, true, dur);
    showDisableModal = false;
    disableTarget = null;
  }

  function formatRemaining(disabledUntil) {
    if (!disabledUntil) return 'permanent';
    const remaining = new Date(disabledUntil) - now;
    if (remaining <= 0) return 're-enabling...';
    const mins = Math.floor(remaining / 60000);
    const secs = Math.floor((remaining % 60000) / 1000);
    if (mins >= 60) {
      const hrs = Math.floor(mins / 60);
      return `${hrs}h ${mins % 60}m`;
    }
    return `${mins}m ${secs}s`;
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

  onMount(() => {
    load();
    const interval = setInterval(() => { now = Date.now(); }, 1000);
    return () => clearInterval(interval);
  });
</script>

{#if showDisableModal}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={() => showDisableModal = false}>
    <div class="bg-gray-900 rounded-lg border border-gray-700 p-6 w-80" onclick={(e) => e.stopPropagation()}>
      <h3 class="text-white font-medium mb-4">
        Disable {disableTarget?.name}
      </h3>
      <label class="block text-gray-400 text-sm mb-2">Duration</label>
      <select bind:value={disableDuration}
              class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm">
        <option value="indefinite">Indefinite</option>
        <option value="10m">10 minutes</option>
        <option value="1h">1 hour</option>
        <option value="24h">24 hours</option>
      </select>
      <div class="flex justify-end gap-2 mt-6">
        <button class="px-3 py-1.5 text-sm text-gray-400 hover:text-white"
                onclick={() => showDisableModal = false}>Cancel</button>
        <button class="px-3 py-1.5 text-sm bg-amber-600 text-white rounded hover:bg-amber-500"
                onclick={confirmDisable}>Disable</button>
      </div>
    </div>
  </div>
{/if}

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
              {#if p.provider_key && p.provider_key !== p.name}
                <span class="rounded bg-gray-800 px-1.5 py-0.5 font-mono text-xs text-gray-400" title="Provider key — use as prefix for direct model access: {p.provider_key}/&lt;model&gt;">
                  {p.provider_key}
                </span>
              {/if}
              {#if p.disabled}
                <span class="inline-flex items-center gap-1.5 text-amber-400 text-sm">
                  <span class="w-2 h-2 rounded-full bg-amber-400"></span>
                  Disabled
                  <span class="text-amber-400/60 text-xs">({formatRemaining(p.disabled_until)})</span>
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
                onclick={() => p.disabled ? toggleDisabled(p.id, false) : openDisableModal(p)}
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