<script>
  import { apiFetch } from '../lib/api.js';

  let { vmId = null, filterExpr = null, sortExpr = null, composition = null, previewMode = false } = $props();

  let resolved = $state([]);
  let apiFilter = $state(null);
  let apiSort = $state(null);
  let loading = $state(false);
  let error = $state('');
  let debounceTimer = null;

  let prevVmId = $state(null);
  let prevFilter = $state(null);
  let prevSort = $state(null);

  async function load() {
    if (previewMode && (filterExpr !== null || composition)) {
      await loadPreview();
      return;
    }
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

  async function loadPreview() {
    loading = true;
    error = '';
    try {
      const body = {};
      if (composition) {
        body.composition = composition;
      } else {
        body.filter_expr = filterExpr || {};
        body.sort_expr = sortExpr || [];
      }
      const data = await apiFetch('/api/v1/virtual-models/preview', {
        method: 'POST',
        body
      });
      resolved = data.models || [];
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function schedulePreview() {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      load();
    }, 300);
  }

  $effect(() => {
    const vId = vmId;
    const f = filterExpr;
    const s = sortExpr;
    const c = composition;
    const pm = previewMode;

    if (pm) {
      schedulePreview();
    } else {
      if (vId !== prevVmId || f !== prevFilter || s !== prevSort) {
        prevVmId = vId;
        prevFilter = f;
        prevSort = s;
        if (vId) load();
      }
    }
  });

  const effFilter = $derived(filterExpr || apiFilter);
  const effSort = $derived(sortExpr || apiSort);

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

  function collectFilterKeys(node) {
    if (!node) return [];
    if (node.key) return [node.key];
    const items = node.and || node.or || [];
    return items.flatMap(i => collectFilterKeys(i));
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

    if (effFilter) {
      for (const key of collectFilterKeys(effFilter)) {
        if (!seen.has(key)) {
          seen.add(key);
          cols.push({ key, abbrev: abbrevKey(key) });
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
