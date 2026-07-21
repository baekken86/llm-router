<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast, metadataFields } from '../lib/stores.js';

  let { provider, model, effort = '', onBack } = $props();

  let loading = $state(true);
  let saving = $state(false);
  let allModels = $state([]);
  let matchingEntries = $state([]);
  let availableEfforts = $state([]);
  let currentModelId = $state(null);
  let overrides = $state({});
  let editedValues = $state({});
  let clearedOverrides = $state(new Set());
  let hasChanges = $state(false);

  const NUMERIC_KEYS = [
    'mc.intelligence', 'mc.coding', 'mc.speed', 'mc.latency',
    'mc.context_window', 'mc.cost_per_1m_input', 'mc.cost_per_1m_output',
    'mc.cost_per_1m_cache', 'mc.cost_per_task', 'mc.hallucination', 'mc.reasoning'
  ];

  const COST_TYPE_OPTIONS = ['', 'free', 'paid', 'cached', 'subscription', 'api-creds'];

  function getImportedValue(key) {
    if (!matchingEntries.length) return null;
    const entry = matchingEntries.find(e => e.reasoning_effort === effort) || matchingEntries[0];
    const raw = key.startsWith('mc.') ? key.slice(3) : key;
    if (entry.tags && raw in entry.tags) return entry.tags[raw];
    if (entry.global_metadata && raw in entry.global_metadata) return entry.global_metadata[raw];
    return null;
  }

  function getAllKeys() {
    const keys = new Set();
    // Include all known metadata fields from the backend
    const fields = $metadataFields;
    if (fields && typeof fields === 'object') {
      for (const k of Object.keys(fields)) {
        keys.add(k);
      }
    }
    // Include keys from matching model entries
    for (const entry of matchingEntries) {
      if (entry.tags) {
        for (const k of Object.keys(entry.tags)) keys.add('mc.' + k);
      }
      if (entry.global_metadata) {
        for (const k of Object.keys(entry.global_metadata)) keys.add(k);
      }
    }
    // Include any override-only keys
    for (const k of Object.keys(overrides)) {
      keys.add(k);
    }
    return [...keys].sort();
  }

  function getCurrentValue(key) {
    if (key in editedValues) return editedValues[key];
    if (key in overrides) return overrides[key];
    return getImportedValue(key) ?? '';
  }

  function isOverridden(key) {
    if (!(key in overrides)) return false;
    const imported = getImportedValue(key);
    return overrides[key] !== imported;
  }

  function hasEditedDiff(key) {
    if (!(key in editedValues)) return false;
    const imported = getImportedValue(key);
    return editedValues[key] !== imported;
  }

  function handleInput(key, value) {
    editedValues = { ...editedValues, [key]: value };
    // Check if this reverts to imported
    const imported = getImportedValue(key);
    if (value === imported) {
      const next = { ...editedValues };
      delete next[key];
      editedValues = next;
    }
    computeHasChanges();
  }

  function clearOverride(key) {
    clearedOverrides = new Set([...clearedOverrides, key]);
    const imported = getImportedValue(key) ?? '';
    editedValues = { ...editedValues, [key]: imported };
    computeHasChanges();
  }

  function computeHasChanges() {
    const keys = getAllKeys();
    for (const key of keys) {
      const edited = key in editedValues ? editedValues[key] : undefined;
      const existingOverride = key in overrides ? overrides[key] : undefined;
      const imported = getImportedValue(key);
      const effective = edited !== undefined ? edited : existingOverride;
      if (effective !== imported) {
        hasChanges = true;
        return;
      }
    }
    // Check if we have overrides that need to be cleared
    for (const key of Object.keys(overrides)) {
      if (key in editedValues) {
        const imported = getImportedValue(key);
        if (editedValues[key] !== imported) { hasChanges = true; return; }
      } else if (overrides[key] !== getImportedValue(key)) {
        hasChanges = true; return;
      }
    }
    hasChanges = false;
  }

  function buildOverridePayload() {
    const payload = {};
    const keys = getAllKeys();
    for (const key of keys) {
      const edited = key in editedValues ? editedValues[key] : undefined;
      const existingOverride = key in overrides ? overrides[key] : undefined;
      const imported = getImportedValue(key);
      const effective = edited !== undefined ? edited : existingOverride;

      // If this key was explicitly cleared, send null to delete the override
      if (clearedOverrides.has(key)) {
        if (key in overrides) {
          const tagKey = key.startsWith('mc.') ? key.slice(3) : key;
          payload[tagKey] = null;
        }
        continue;
      }

      if (effective !== imported) {
        const tagKey = key.startsWith('mc.') ? key.slice(3) : key;
        payload[tagKey] = effective === '' || effective === null ? null : effective;
      }
    }
    return payload;
  }

  async function save() {
    if (!currentModelId) return;
    saving = true;
    try {
      const payload = buildOverridePayload();
      await apiFetch(`/api/v1/models/${currentModelId}/overrides?effort=${encodeURIComponent(effort)}`, {
        method: 'PUT',
        body: { tags: payload }
      });
      addToast('Overrides saved', 'success');
      // Reload overrides to sync state
      await loadOverrides();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function clearAll() {
    if (!currentModelId) return;
    saving = true;
    try {
      await apiFetch(`/api/v1/models/${currentModelId}/overrides?effort=${encodeURIComponent(effort)}`, {
        method: 'DELETE'
      });
      addToast('Overrides cleared', 'success');
      await loadOverrides();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function loadOverrides() {
    if (!currentModelId) return;
    try {
      const data = await apiFetch(`/api/v1/models/${currentModelId}/overrides?effort=${encodeURIComponent(effort)}`);
      const raw = data.overrides || {};
      const normalized = {};
      for (const [k, v] of Object.entries(raw)) {
        const prefixed = k.includes('.') ? k : 'mc.' + k;
        normalized[prefixed] = v;
      }
      overrides = normalized;
      editedValues = {};
      hasChanges = false;
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  onMount(async () => {
    try {
      allModels = await apiFetch('/api/v1/models');
      matchingEntries = allModels.filter(m => m.provider_name === provider && m.model_name === model);
      availableEfforts = [...new Set(matchingEntries.map(m => m.reasoning_effort).filter(Boolean))].sort();
      const entry = matchingEntries.find(m => m.reasoning_effort === effort) || matchingEntries[0];
      if (entry) currentModelId = entry.model_id;
      await loadOverrides();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  });

  $effect(() => {
    const e = effort;
    const mid = currentModelId;
    if (!loading && mid) {
      const entry = matchingEntries.find(m => m.reasoning_effort === e) || matchingEntries[0];
      if (entry && entry.model_id !== mid) {
        currentModelId = entry.model_id;
      }
      loadOverrides();
    }
  });
</script>

<div>
  <div class="flex items-center gap-4 mb-6">
    <a
      href="#"
      class="text-sm text-gray-400 hover:text-white no-underline"
      onclick={(e) => { e.preventDefault(); onBack(); }}
    >
      &larr; Back
    </a>
    <h2 class="text-xl font-bold text-gray-100">
      <span class="text-emerald-400">{provider}</span>
      <span class="text-gray-500 mx-2">/</span>
      <span>{model}</span>
      {#if effort}
        <span class="text-gray-500 mx-2">/</span>
        <span class="text-amber-400">{effort}</span>
      {/if}
    </h2>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if !currentModelId}
    <p class="text-gray-500">No matching model entries found.</p>
  {:else}
    {#if availableEfforts.length > 1}
      <div class="mb-4 flex items-center gap-3">
        <label class="text-sm text-gray-400">Effort:</label>
        <select
          class="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
          value={effort}
          onchange={(e) => onBack(provider, model, e.target.value)}
        >
          {#each availableEfforts as eff}
            <option value={eff}>{eff}</option>
          {/each}
        </select>
      </div>
    {/if}

    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <table class="w-full text-sm font-mono">
        <thead>
          <tr class="text-gray-500 border-b border-gray-800">
            <th class="text-left px-4 py-2">Key</th>
            <th class="text-left px-4 py-2">Imported</th>
            <th class="text-left px-4 py-2">Current</th>
            <th class="w-10 px-2 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {#each getAllKeys() as key}
            {@const imported = getImportedValue(key)}
            {@const currentVal = getCurrentValue(key)}
            {@const overridden = hasEditedDiff(key) || (key in overrides && overrides[key] !== imported && !clearedOverrides.has(key))}
            <tr class="border-b border-gray-850 {overridden ? 'bg-amber-900/30' : ''}">
              <td class="px-4 py-2 text-gray-300">{key}</td>
              <td class="px-4 py-2 text-gray-500">{imported ?? '—'}</td>
              <td class="px-4 py-2 {overridden ? 'border-amber-700 border text-amber-200' : ''}">
                {#if key === 'mc.cost_type'}
                  <select
                    class="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
                    value={currentVal ?? ''}
                    onchange={(e) => handleInput(key, e.target.value)}
                  >
                    {#each COST_TYPE_OPTIONS as opt}
                      <option value={opt}>{opt || '(imported)'}</option>
                    {/each}
                  </select>
                {:else if NUMERIC_KEYS.includes(key)}
                  <input
                    type="number"
                    step="0.01"
                    class="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-gray-100 w-32 focus:outline-none focus:border-emerald-500"
                    value={currentVal ?? ''}
                    oninput={(e) => handleInput(key, e.target.value)}
                  />
                {:else}
                  <input
                    type="text"
                    class="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-gray-100 w-48 focus:outline-none focus:border-emerald-500"
                    value={currentVal ?? ''}
                    oninput={(e) => handleInput(key, e.target.value)}
                  />
                {/if}
              </td>
              <td class="px-2 py-2 text-center">
                {#if overridden || (key in overrides) || (key in editedValues)}
                  <button
                    class="text-gray-500 hover:text-red-400 text-xs px-1"
                    title="Clear override"
                    onclick={() => clearOverride(key)}
                  >×</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <div class="flex gap-3 mt-4">
      <button
        class="bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white rounded px-6 py-2 text-sm font-medium"
        disabled={saving || !hasChanges}
        onclick={save}
      >
        {saving ? 'Saving...' : 'Save changes'}
      </button>
      <button
        class="bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 rounded px-6 py-2 text-sm font-medium"
        disabled={saving}
        onclick={clearAll}
      >
        Clear all overrides
      </button>
    </div>
  {/if}
</div>
