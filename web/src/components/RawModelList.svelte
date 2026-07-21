<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';
  import ModelPicker from './ModelPicker.svelte';

  let {
    onEditMetadata = () => {},
  } = $props();

  let models = $state([]);
  let loading = $state(true);
  let expandedProviders = $state({});
  let pickerModelId = $state(null);
  let unmapConfirmId = $state(null);

  function abbrevKey(key) {
    return key;
  }

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

  const modelCount = $derived(() => {
    const seen = new Set();
    for (const m of models) {
      seen.add(m.model_id);
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
            cols.push({ key: prefixed, abbrev: abbrevKey(prefixed) });
          }
        }
      }
      if (m.global_metadata) {
        for (const key of Object.keys(m.global_metadata)) {
          const prefixed = 'm.' + key;
          if (!seen.has(prefixed)) {
            seen.add(prefixed);
            cols.push({ key: prefixed, abbrev: abbrevKey(prefixed) });
          }
        }
      }
    }
    return cols;
  });

  function getTagValue(entry, colKey) {
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

  async function load() {
    loading = true;
    try {
      models = await apiFetch('/api/v1/models');
      for (const p of providers) {
        expandedProviders[p] = true;
      }
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  onMount(() => { load(); });
</script>

{#if pickerModelId !== null}
  <ModelPicker
    excludeModelName={models.find(m => m.model_id === pickerModelId)?.model_name || ''}
    onSelect={(targetName) => createMapping(pickerModelId, targetName)}
    onClose={() => pickerModelId = null}
  />
{/if}

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Raw Models</h2>
    <span class="text-sm text-gray-500">{models.length} rows across {providers.length} providers ({modelCount()} models)</span>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if models.length === 0}
    <p class="text-gray-500">No raw models found. Discover models from providers first.</p>
  {:else}
    <div class="space-y-4">
      {#each providers as provider}
        <div class="bg-gray-900 border border-gray-800 rounded-lg">
          <div
            class="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-gray-850"
            onclick={() => toggleProvider(provider)}
          >
            <div class="flex items-center gap-3">
              <span class="text-gray-500">{expandedProviders[provider] ? '▼' : '▶'}</span>
              <span class="font-medium text-emerald-400">{provider}</span>
              <span class="text-xs text-gray-500">{grouped[provider].length} rows</span>
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
                      {#each columns as col}
                        <th class="text-right pr-3 py-1">{col.abbrev}</th>
                      {/each}
                    </tr>
                  </thead>
                  <tbody>
                    {#each grouped[provider] as m}
                      <tr class="border-b border-gray-850 hover:bg-gray-850/50">
                        <td class="text-left pr-3 py-1">
                          <a
                            href="#"
                            class="text-emerald-400 hover:text-emerald-300 no-underline"
                            onclick={(e) => { e.preventDefault(); onEditMetadata(m.provider_name, m.model_name, m.reasoning_effort); }}
                          >{m.model_name}</a>
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
                        {#each columns as col}
                          <td class="text-right pr-3 py-1 text-gray-300">{getTagValue(m, col.key)}</td>
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
