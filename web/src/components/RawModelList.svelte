<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ModelPicker from './ModelPicker.svelte';

  let models = $state([]);
  let loading = $state(true);
  let expandedProviders = $state({});
  let pickerModelId = $state(null);
  let unmapConfirmId = $state(null);
  let refreshing = $state(false);
  let showDisableModal = $state(false);
  let disableTarget = $state(null);
  let disableDuration = $state('10m');
  let now = $state(Date.now());
  let allOverrides = $state({});

  const grouped = $derived.by(() => {
    const groups = {};
    for (const m of models) {
      const provider = m.provider_name || 'unknown';
      if (!groups[provider]) groups[provider] = [];
      groups[provider].push(m);
    }
    for (const provider of Object.keys(groups)) {
      groups[provider].sort((a, b) => a.model_name.localeCompare(b.model_name) || a.reasoning_effort.localeCompare(b.reasoning_effort));
    }
    return groups;
  });

  const providers = $derived(Object.keys(grouped).sort());

  const modelCount = $derived.by(() => {
    const seen = new Set();
    for (const m of models) {
      seen.add(m.model_id);
    }
    return seen.size;
  });

  const enabledModelCount = $derived.by(() => {
    const seen = new Set();
    for (const m of models) {
      if (!m.disabled) seen.add(m.model_id);
    }
    return seen.size;
  });

  const columns = $derived.by(() => {
    const seen = new Set();
    const cols = [];
    for (const m of models) {
      if (m.tags) {
        for (const key of Object.keys(m.tags)) {
          const prefixed = 'mc.' + key;
          if (!seen.has(prefixed)) {
            seen.add(prefixed);
            cols.push({ key: prefixed });
          }
        }
      }
      if (m.global_metadata) {
        for (const key of Object.keys(m.global_metadata)) {
          const prefixed = 'm.' + key;
          if (!seen.has(prefixed)) {
            seen.add(prefixed);
            cols.push({ key: prefixed });
          }
        }
      }
    }
    return cols;
  });

  function getOverrideForCell(entry, colKey) {
    const effortKey = `${entry.model_id}:${entry.reasoning_effort}`;
    const overrides = allOverrides[effortKey];
    if (!overrides) return undefined;
    const raw = colKey.startsWith('mc.') ? colKey.slice(3) : colKey.startsWith('m.') ? colKey.slice(2) : colKey;
    return raw in overrides ? overrides[raw] : undefined;
  }

  function getTagValue(entry, colKey) {
    const override = getOverrideForCell(entry, colKey);
    if (override !== undefined) return override;
    if (colKey.startsWith('mc.')) {
      const raw = colKey.slice(3);
      return entry.tags?.[raw] || '-';
    }
    if (colKey.startsWith('m.')) {
      const raw = colKey.slice(2);
      return entry.global_metadata?.[raw] || '-';
    }
    return entry.global_metadata?.[colKey] || entry.tags?.[colKey] || '-';
  }

  function getImportedValue(entry, colKey) {
    if (colKey.startsWith('mc.')) {
      const raw = colKey.slice(3);
      return entry.tags?.[raw] ?? null;
    }
    if (colKey.startsWith('m.')) {
      const raw = colKey.slice(2);
      return entry.global_metadata?.[raw] ?? null;
    }
    return null;
  }

  function isCellOverriddenStatic(entry, colKey) {
    const imported = getImportedValue(entry, colKey);
    const override = getOverrideForCell(entry, colKey);
    if (override === undefined) return false;
    return String(override) !== String(imported ?? '');
  }

  function toggleProvider(provider) {
    expandedProviders[provider] = !expandedProviders[provider];
  }

  async function createMapping(sourceId, targetName) {
    try {
      await apiFetch(`/api/v1/models/${sourceId}/mapping`, {
        method: 'POST',
        body: { target_model_name: targetName }
      });
      addToast('Mapping created', 'success');
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function deleteMapping(sourceId) {
    try {
      await apiFetch(`/api/v1/models/${sourceId}/mapping`, { method: 'DELETE' });
      addToast('Mapping deleted', 'success');
      unmapConfirmId = null;
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function toggleDisabled(modelId, disabled, duration) {
    try {
      const body = { disabled };
      if (disabled && duration) body.duration = duration;
      await apiFetch(`/api/v1/models/${modelId}/disabled`, {
        method: 'PUT',
        body
      });
      models = models.map(m => {
        if (m.model_id === modelId) {
          const updated = { ...m, disabled };
          if (disabled && duration) {
            const ms = { '10m': 600000, '1h': 3600000, '24h': 86400000 }[duration];
            updated.disabled_until = new Date(Date.now() + ms).toISOString();
          } else if (disabled) {
            updated.disabled_until = null;
          } else {
            updated.disabled_until = null;
          }
          return updated;
        }
        return m;
      });
      addToast(disabled ? 'Model disabled' : 'Model enabled', 'success');
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  function openDisableModal(model) {
    disableTarget = model;
    disableDuration = '10m';
    showDisableModal = true;
  }

  async function confirmDisable() {
    if (!disableTarget) return;
    const dur = disableDuration === 'indefinite' ? null : disableDuration;
    await toggleDisabled(disableTarget.model_id, true, dur);
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

  async function refreshAll() {
    refreshing = true;
    try {
      const results = await apiFetch('/api/v1/providers/discover-all', { method: 'POST' });
      let totalDeactivated = 0;
      for (const r of results) {
        if (r.error) {
          addToast(`${r.name}: ${r.error}`, 'error');
        } else {
          totalDeactivated += r.deactivated;
        }
      }
      if (totalDeactivated > 0) {
        addToast(`Refreshed ${results.length} providers — ${totalDeactivated} stale models deactivated`, 'success');
      } else {
        addToast(`Refreshed ${results.length} providers — all models up to date`, 'success');
      }
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      refreshing = false;
    }
  }

  async function loadAllOverrides() {
    const effortKeys = new Map();
    for (const m of models) {
      const k = `${m.model_id}:${m.reasoning_effort}`;
      if (!effortKeys.has(k)) effortKeys.set(k, m);
    }
    const results = await Promise.allSettled(
      [...effortKeys.entries()].map(async ([k, m]) => {
        const data = await apiFetch(`/api/v1/models/${m.model_id}/overrides?effort=${encodeURIComponent(m.reasoning_effort)}`);
        return [k, data.overrides || {}];
      })
    );
    const overrides = {};
    for (const r of results) {
      if (r.status === 'fulfilled') {
        overrides[r.value[0]] = r.value[1];
      }
    }
    allOverrides = overrides;
  }

  async function load() {
    loading = true;
    try {
      models = await apiFetch('/api/v1/models');
      for (const p of providers) {
        expandedProviders[p] = true;
      }
      await loadAllOverrides();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
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
        Disable {disableTarget?.model_name}
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

{#if pickerModelId !== null}
  <ModelPicker
    excludeModelName={models.find(m => m.model_id === pickerModelId)?.model_name || ''}
    onSelect={(targetName) => createMapping(pickerModelId, targetName)}
    onClose={() => pickerModelId = null}
  />
{/if}

<div>
  <div class="flex items-center justify-between mb-6">
    <div class="flex items-center gap-4">
      <h2 class="text-xl font-bold text-gray-100">Providers</h2>
      <button
        class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition disabled:opacity-50 disabled:cursor-not-allowed"
        onclick={refreshAll}
        disabled={refreshing}
      >
        {refreshing ? 'Refreshing...' : 'Refresh All'}
      </button>
    </div>
    <span class="text-sm text-gray-500">{models.length} rows across {providers.length} providers ({enabledModelCount} / {modelCount} models active)</span>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if models.length === 0}
    <p class="text-gray-500">No models found. Discover models from providers first.</p>
  {:else}
    <div class="space-y-4">
      {#each providers as provider}
        {@const providerModels = grouped[provider]}
        {@const providerEnabledCount = providerModels.filter(m => !m.disabled).length}
        <div class="bg-gray-900 border border-gray-800 rounded-lg">
          <div
            class="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-gray-850"
            onclick={() => toggleProvider(provider)}
          >
            <div class="flex items-center gap-3">
              <span class="text-gray-500">{expandedProviders[provider] ? '▼' : '▶'}</span>
              <span class="font-medium text-emerald-400">{provider}</span>
              <span class="text-xs text-gray-500">{providerEnabledCount} / {providerModels.length} active</span>
            </div>
          </div>
          {#if expandedProviders[provider]}
            <div class="px-4 pb-3 border-t border-gray-800 pt-3">
              <div class="overflow-x-auto">
                <table class="w-full text-sm font-mono">
                  <thead>
                    <tr class="text-gray-500 border-b border-gray-800">
                      <th class="text-left pr-3 py-1">model</th>
                      <th class="text-left pr-3 py-1">effort</th>
                      <th class="text-left pr-3 py-1">mapping</th>
                      <th class="text-left pr-3 py-1 w-16">status</th>
                      {#each columns as col}
                        <th class="text-right pr-3 py-1">{col.key}</th>
                      {/each}
                    </tr>
                  </thead>
                  <tbody>
                    {#each grouped[provider] as m}
                      <tr class="border-b border-gray-850 hover:bg-gray-850/50 {m.disabled ? 'opacity-40' : ''}">
                        <td class="text-left pr-3 py-1">
                          <span class="text-gray-300 {m.disabled ? 'line-through' : ''}">{m.model_name}</span>
                        </td>
                        <td class="text-left pr-3 py-1 text-gray-400">{m.reasoning_effort || '—'}</td>
                        <td class="text-left pr-3 py-1">
                          {#if m.mapping_target_name}
                            <span class="inline-flex items-center gap-1">
                              <span class="text-xs bg-blue-900/50 text-blue-300 px-2 py-0.5 rounded">
                                → {m.mapping_target_name}
                              </span>
                              {#if unmapConfirmId === m.model_id}
                                <button
                                  class="text-xs text-red-400 hover:text-red-300"
                                  onclick={() => deleteMapping(m.model_id)}
                                >Yes</button>
                                <button
                                  class="text-xs text-gray-500 hover:text-gray-300"
                                  onclick={() => unmapConfirmId = null}
                                >No</button>
                              {:else}
                                <button
                                  class="text-xs text-gray-500 hover:text-red-400"
                                  onclick={() => unmapConfirmId = m.model_id}
                                >✕</button>
                              {/if}
                            </span>
                          {:else}
                            <button
                              class="text-xs text-gray-600 hover:text-emerald-400"
                              onclick={() => pickerModelId = m.model_id}
                            >🔗 map</button>
                          {/if}
                        </td>
                        <td class="text-left pr-3 py-1">
                          <span class="inline-flex items-center gap-1.5">
                            <button
                              class="text-xs px-2 py-0.5 rounded transition {m.disabled ? 'bg-gray-800 text-gray-500 hover:text-emerald-400' : 'bg-emerald-900/50 text-emerald-400 hover:text-red-400'}"
                              onclick={() => m.disabled ? toggleDisabled(m.model_id, false) : openDisableModal(m)}
                              title={m.disabled ? 'Enable model' : 'Disable model'}
                            >
                              {m.disabled ? 'enable' : 'disable'}
                            </button>
                            {#if m.disabled}
                              <span class="text-xs text-amber-400/70">
                                {formatRemaining(m.disabled_until)}
                              </span>
                            {/if}
                          </span>
                        </td>
                        {#each columns as col}
                          {@const overridden = isCellOverriddenStatic(m, col.key)}
                          <td class="text-right pr-3 py-1 {overridden ? 'text-amber-200 bg-amber-900/20' : 'text-gray-300'}">{getTagValue(m, col.key)}</td>
                        {/each}
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
