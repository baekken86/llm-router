<script>
  import { apiFetch } from '../lib/api.js';

  let { vmId = null, filterExpr = null, sortExpr = null } = $props();

  let resolved = $state([]);
  let apiFilter = $state(null);
  let apiSort = $state(null);
  let loading = $state(false);
  let error = $state('');

  async function load() {
    if (!vmId) return;
    loading = true;
    error = '';
    try {
      const data = await apiFetch(`/api/v1/virtual-models/${vmId}/resolved`);
      resolved = data.models || [];
      if (!filterExpr) apiFilter = data.filter;
      if (!sortExpr) apiSort = data.sort;
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    if (vmId) load();
  });

  const effFilter = $derived(filterExpr || apiFilter);
  const effSort = $derived(sortExpr || apiSort);

  function abbrevKey(key) {
    const map = {
      intelligence: 'int',
      coding: 'code',
      speed: 'spd',
      cost_per_task: '$/task',
      cost_per_1m_input: '$/1M',
      cost_per_1m_output: '$/1M.out',
      cost_per_1m_cache: '$/1M.cache',
      hallucination: 'hall',
      latency: 'lat',
      context_window: 'ctx',
      cost_type: 'cost_type',
      has_reasoning_effort: 'has_effort',
      reasoning: 'reason',
    };
    return map[key] || key;
  }

  const columns = $derived.by(() => {
    const seen = new Set();
    const cols = [];

    if (effSort && Array.isArray(effSort)) {
      for (const s of effSort) {
        if (s.key && !seen.has(s.key)) {
          seen.add(s.key);
          cols.push({ key: s.key, abbrev: abbrevKey(s.key) });
        }
      }
    }

    if (effFilter && effFilter.and) {
      for (const f of effFilter.and) {
        if (f.key && !seen.has(f.key)) {
          seen.add(f.key);
          cols.push({ key: f.key, abbrev: abbrevKey(f.key) });
        }
      }
    }

    for (const m of resolved) {
      if (m.tags) {
        for (const k of Object.keys(m.tags)) {
          if (!seen.has(k)) {
            seen.add(k);
            cols.push({ key: k, abbrev: abbrevKey(k) });
          }
        }
      }
    }

    return cols;
  });
</script>

{#if loading}
  <p class="text-sm text-gray-500">Loading resolved models...</p>
{:else if error}
  <p class="text-sm text-red-400">{error}</p>
{:else if resolved.length === 0}
  <p class="text-sm text-gray-500">No matching models found.</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-sm font-mono">
      <thead>
        <tr class="text-gray-500 border-b border-gray-800">
          <th class="text-right pr-3 py-1">#</th>
          <th class="text-left pr-3 py-1">provider</th>
          <th class="text-left pr-3 py-1">model</th>
          <th class="text-left pr-3 py-1">effort</th>
          {#each columns as col}
            <th class="text-right pr-3 py-1">{col.abbrev}</th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each resolved as m}
          <tr class="border-b border-gray-850 hover:bg-gray-850/50">
            <td class="text-right pr-3 py-1 text-gray-500">{m.position}</td>
            <td class="text-left pr-3 py-1 text-gray-400">{m.provider_name}</td>
            <td class="text-left pr-3 py-1 text-emerald-400">{m.model_name}</td>
            <td class="text-left pr-3 py-1 text-gray-400">{m.reasoning_effort || '-'}</td>
            {#each columns as col}
              <td class="text-right pr-3 py-1 text-gray-300">{m.tags?.[col.key] || '-'}</td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
