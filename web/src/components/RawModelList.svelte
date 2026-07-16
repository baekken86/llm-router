<script>
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let models = $state([]);
  let loading = $state(true);
  let expandedProviders = $state({});

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

  $effect(() => { load(); });
</script>

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
                      {#each columns as col}
                        <th class="text-right pr-3 py-1">{col.abbrev}</th>
                      {/each}
                    </tr>
                  </thead>
                  <tbody>
                    {#each grouped[provider] as m}
                      <tr class="border-b border-gray-850 hover:bg-gray-850/50">
                        <td class="text-left pr-3 py-1 text-emerald-400">{m.name}</td>
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
