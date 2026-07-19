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

  function abbrevKey(key) {
    const map = {
      'mc.intelligence': 'int',
      'mc.coding': 'code',
      'mc.speed': 'spd',
      'mc.cost_per_task': '$/task',
      'mc.cost_per_1m_input': '$/1M',
      'mc.cost_per_1m_output': '$/1M.out',
      'mc.cost_per_1m_cache': '$/1M.cache',
      'mc.hallucination': 'hall',
      'mc.latency': 'lat',
      'mc.context_window': 'ctx',
      'mc.cost_type': 'cost_type',
      'mc.has_reasoning_effort': 'has_effort',
      'mc.reasoning': 'reason',
    };
    return map[key] || key;
  }

  const grouped = $derived.by(() => {
    const groups = {};
    for (const m of models) {
      const provider = m.provider_name || 'unknown';
      if (!groups[provider]) groups[provider] = [];
      groups[provider].push(m);
    }
    for (const provider of Object.keys(groups)) {
      groups[provider].sort((a, b) => a.name.localeCompare(b.name));
    }
    return groups;
  });

  const providers = $derived(Object.keys(grouped).sort());

  const columns = $derived.by(() => {
    const seen = new Set();
    const cols = [];
    for (const m of models) {
      if (m.tags) {
        for (const t of m.tags) {
          const key = t.key;
          if (!seen.has(key)) {
            seen.add(key);
            cols.push({ key, abbrev: abbrevKey(key) });
          }
        }
      }
    }
    return cols;
  });

  function getTagValue(model, key) {
    if (!model.tags) return '-';
    const tag = model.tags.find(t => t.key === key);
    return tag ? tag.value : '-';
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
    excludeModelName={models.find(m => m.id === pickerModelId)?.name || ''}
    onSelect={(targetName) => createMapping(pickerModelId, targetName)}
    onClose={() => pickerModelId = null}
  />
{/if}

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Raw Models</h2>
    <span class="text-sm text-gray-500">{models.length} models across {providers.length} providers</span>
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
              <span class="text-xs text-gray-500">{grouped[provider].length} models</span>
            </div>
          </div>
          {#if expandedProviders[provider]}
            <div class="px-4 pb-3 border-t border-gray-800 pt-3">
              <div class="overflow-x-auto">
                <table class="w-full text-sm font-mono">
                  <thead>
                    <tr class="text-gray-500 border-b border-gray-800">
                      <th class="text-left pr-3 py-1">model</th>
                      <th class="text-left pr-3 py-1">mapping</th>
                      {#each columns as col}
                        <th class="text-right pr-3 py-1">{col.abbrev}</th>
                      {/each}
                    </tr>
                  </thead>
                  <tbody>
                    {#each grouped[provider] as m}
                      <tr class="border-b border-gray-850 hover:bg-gray-850/50">
                        <td class="text-left pr-3 py-1 text-emerald-400">{m.name}</td>
                        <td class="text-left pr-3 py-1">
                          {#if m.mapping_target_name}
                            <span class="inline-flex items-center gap-1">
                              <span class="text-xs bg-blue-900/50 text-blue-300 px-2 py-0.5 rounded">
                                → {m.mapping_target_name}
                              </span>
                              {#if unmapConfirmId === m.id}
                                <button
                                  class="text-xs text-red-400 hover:text-red-300"
                                  onclick={() => deleteMapping(m.id)}
                                >Yes</button>
                                <button
                                  class="text-xs text-gray-500 hover:text-gray-300"
                                  onclick={() => unmapConfirmId = null}
                                >No</button>
                              {:else}
                                <button
                                  class="text-xs text-gray-500 hover:text-red-400"
                                  onclick={() => unmapConfirmId = m.id}
                                >✕</button>
                              {/if}
                            </span>
                          {:else}
                            <button
                              class="text-xs text-gray-600 hover:text-emerald-400"
                              onclick={() => pickerModelId = m.id}
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
