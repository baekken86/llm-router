<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast, metadataFields } from '../lib/stores.js';
  import { getFieldType, getFieldValues } from '../lib/fields.js';

  let entries = $state([]);
  let loading = $state(true);
  let editMode = $state(false);
  let editValues = $state({});
  let saving = $state(false);
  let filterText = $state('');
  let showAddModal = $state(false);
  let newModelName = $state('');
  let newEffort = $state('');
  let newMetadata = $state({});

  const columns = $derived.by(() => {
    const seen = new Set();
    const cols = [];
    // Include fields from existing entries
    for (const entry of entries) {
      if (entry.metadata) {
        for (const key of Object.keys(entry.metadata)) {
          const prefixed = 'mc.' + key;
          if (!seen.has(prefixed)) {
            seen.add(prefixed);
            cols.push({ key: prefixed });
          }
        }
      }
    }
    // Also include mc.* fields from the global field definitions (for fresh installs)
    const fields = $metadataFields;
    if (fields && typeof fields === 'object') {
      for (const key of Object.keys(fields)) {
        if (key.startsWith('mc.') && !seen.has(key)) {
          seen.add(key);
          cols.push({ key });
        }
      }
    }
    return cols;
  });

  const filteredEntries = $derived.by(() => {
    let list = [...entries].sort((a, b) =>
      a.model_name.localeCompare(b.model_name) || a.reasoning_effort.localeCompare(b.reasoning_effort)
    );
    if (filterText.trim()) {
      const needle = filterText.trim().toLowerCase();
      list = list.filter(e => e.model_name.toLowerCase().includes(needle));
    }
    return list;
  });

  function getOriginalValue(entry, colKey) {
    const raw = colKey.startsWith('mc.') ? colKey.slice(3) : colKey;
    return entry.metadata?.[raw] ?? '';
  }

  function overrideKey(entryIdx, colKey) {
    return `${entryIdx}:${colKey}`;
  }

  function getEffectiveValue(entry, entryIdx, colKey) {
    const key = overrideKey(entryIdx, colKey);
    if (key in editValues) return editValues[key];
    return getOriginalValue(entry, colKey);
  }

  function isCellEdited(entry, entryIdx, colKey) {
    const key = overrideKey(entryIdx, colKey);
    if (!(key in editValues)) return false;
    return editValues[key] !== getOriginalValue(entry, colKey);
  }

  function handleCellInput(entryIdx, colKey, value) {
    const key = overrideKey(entryIdx, colKey);
    editValues = { ...editValues, [key]: value };
  }

  const editHasChanges = $derived.by(() => {
    for (let i = 0; i < filteredEntries.length; i++) {
      for (const col of columns) {
        const key = overrideKey(i, col.key);
        if (key in editValues) {
          const original = getOriginalValue(filteredEntries[i], col.key);
          if (editValues[key] !== original) return true;
        }
      }
    }
    return false;
  });

  function enterEditMode() {
    editMode = true;
    editValues = {};
  }

  function exitEditMode() {
    editMode = false;
    editValues = {};
  }

  async function submitChanges() {
    saving = true;
    const byEntry = new Map();
    for (let i = 0; i < filteredEntries.length; i++) {
      const entry = filteredEntries[i];
      const entryKey = `${entry.model_name}||${entry.reasoning_effort}`;
      if (!byEntry.has(entryKey)) {
        byEntry.set(entryKey, { ...entry, changes: {} });
      }
    }
    for (const [key, value] of Object.entries(editValues)) {
      const sepIdx = key.indexOf(':');
      const idx = parseInt(key.substring(0, sepIdx), 10);
      const colKey = key.substring(sepIdx + 1);
      const entry = filteredEntries[idx];
      if (!entry) continue;
      const entryKey = `${entry.model_name}||${entry.reasoning_effort}`;
      if (!byEntry.has(entryKey)) continue;
      const raw = colKey.startsWith('mc.') ? colKey.slice(3) : colKey;
      byEntry.get(entryKey).changes[raw] = value;
    }

    let successCount = 0;
    let errorCount = 0;
    const requests = [];
    for (const item of byEntry.values()) {
      if (Object.keys(item.changes).length === 0) continue;
      const mergedMetadata = { ...item.metadata, ...item.changes };
      requests.push(
        apiFetch('/api/v1/model-metadata', {
          method: 'PUT',
          body: {
            model_name: item.model_name,
            reasoning_effort: item.reasoning_effort,
            metadata: mergedMetadata
          }
        }).then(() => successCount++).catch(() => errorCount++)
      );
    }
    await Promise.allSettled(requests);
    if (errorCount > 0) {
      addToast(`${successCount} saved, ${errorCount} failed`, 'error');
    } else if (successCount > 0) {
      addToast(`${successCount} model metadata saved`, 'success');
    }
    exitEditMode();
    await load();
  }

  async function addNewEntry() {
    if (!newModelName.trim()) {
      addToast('Model name is required', 'error');
      return;
    }
    saving = true;
    try {
      const metadata = {};
      for (const [key, value] of Object.entries(newMetadata)) {
        if (value !== '') {
          const raw = key.startsWith('mc.') ? key.slice(3) : key;
          metadata[raw] = value;
        }
      }
      await apiFetch('/api/v1/model-metadata', {
        method: 'PUT',
        body: {
          model_name: newModelName.trim(),
          reasoning_effort: newEffort.trim(),
          metadata
        }
      });
      addToast('Model entry created', 'success');
      showAddModal = false;
      newModelName = '';
      newEffort = '';
      newMetadata = {};
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function load() {
    loading = true;
    try {
      entries = await apiFetch('/api/v1/model-metadata');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load();
  });
</script>

{#if showAddModal}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick={() => showAddModal = false}>
    <div class="bg-gray-900 rounded-lg border border-gray-700 p-6 w-[32rem] max-h-[80vh] overflow-y-auto" onclick={(e) => e.stopPropagation()}>
      <h3 class="text-white font-medium mb-4">Add Model Entry</h3>
      <div class="space-y-4">
        <div>
          <label class="block text-gray-400 text-sm mb-1">Model Name *</label>
          <input
            type="text"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm focus:outline-none focus:border-emerald-500"
            placeholder="e.g. gpt-4o"
            bind:value={newModelName}
          />
        </div>
        <div>
          <label class="block text-gray-400 text-sm mb-1">Reasoning Effort</label>
          <input
            type="text"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm focus:outline-none focus:border-emerald-500"
            placeholder="e.g. high, medium, low (or leave empty)"
            bind:value={newEffort}
          />
        </div>
        {#each columns as col}
          {@const fieldType = getFieldType($metadataFields, col.key)}
          {@const fieldValues = getFieldValues($metadataFields, col.key)}
          <div>
            <label class="block text-gray-400 text-sm mb-1">{col.key}</label>
            {#if fieldValues}
              <select
                class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm focus:outline-none focus:border-emerald-500"
                bind:value={newMetadata[col.key]}
              >
                <option value="">—</option>
                {#each fieldValues as val}
                  <option value={val}>{val}</option>
                {/each}
              </select>
            {:else if fieldType === 'number'}
              <input
                type="number"
                step="0.01"
                class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm focus:outline-none focus:border-emerald-500"
                bind:value={newMetadata[col.key]}
              />
            {:else}
              <input
                type="text"
                class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm focus:outline-none focus:border-emerald-500"
                bind:value={newMetadata[col.key]}
              />
            {/if}
          </div>
        {/each}
      </div>
      <div class="flex justify-end gap-2 mt-6">
        <button class="px-3 py-1.5 text-sm text-gray-400 hover:text-white"
                onclick={() => showAddModal = false}>Cancel</button>
        <button class="px-3 py-1.5 text-sm bg-emerald-600 text-white rounded hover:bg-emerald-500 disabled:opacity-50"
                disabled={saving || !newModelName.trim()}
                onclick={addNewEntry}>
          {saving ? 'Adding...' : 'Add Entry'}
        </button>
      </div>
    </div>
  </div>
{/if}

<div>
  <div class="flex items-center justify-between mb-6">
    <div class="flex items-center gap-4">
      <h2 class="text-xl font-bold text-gray-100">Models</h2>
      {#if editMode}
        <button
          class="px-3 py-1.5 text-sm bg-emerald-600 text-white rounded hover:bg-emerald-700 transition disabled:opacity-50 disabled:cursor-not-allowed"
          disabled={saving || !editHasChanges}
          onclick={submitChanges}
        >
          {saving ? 'Saving...' : 'Submit Changes'}
        </button>
        <button
          class="px-3 py-1.5 text-sm bg-gray-700 text-gray-300 rounded hover:bg-gray-600 hover:text-white transition disabled:opacity-50"
          disabled={saving}
          onclick={exitEditMode}
        >
          Cancel
        </button>
      {:else}
        <button
          class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
          onclick={enterEditMode}
        >
          Edit Overrides
        </button>
        <button
          class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
          onclick={() => { showAddModal = true; newMetadata = {}; }}
        >
          Add Model
        </button>
      {/if}
    </div>
    <div class="flex items-center gap-4">
      <input
        type="text"
        class="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:border-emerald-500 w-48"
        placeholder="Filter models..."
        bind:value={filterText}
      />
      <span class="text-sm text-gray-500">
        {filteredEntries.length}{filterText.trim() ? ` / ${entries.length}` : ''} model+effort entries
      </span>
    </div>
  </div>

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if entries.length === 0}
    <p class="text-gray-500">No model metadata entries found. Add models manually or import from data/models.json.</p>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm font-mono">
          <thead>
            <tr class="text-gray-500 border-b border-gray-800">
              <th class="text-left px-4 py-2">model</th>
              <th class="text-left px-4 py-2">effort</th>
              {#each columns as col}
                <th class="text-right px-4 py-2">{col.key}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each filteredEntries as entry, entryIdx}
              <tr class="border-b border-gray-850 hover:bg-gray-850/50">
                <td class="text-left px-4 py-1.5 text-gray-300">{entry.model_name}</td>
                <td class="text-left px-4 py-1.5 text-gray-400">{entry.reasoning_effort || '—'}</td>
                {#each columns as col}
                  {#if editMode}
                    {@const effective = getEffectiveValue(entry, entryIdx, col.key)}
                    {@const edited = isCellEdited(entry, entryIdx, col.key)}
                    {@const fieldType = getFieldType($metadataFields, col.key)}
                    {@const fieldValues = getFieldValues($metadataFields, col.key)}
                    <td class="text-right px-4 py-1.5 {edited ? 'bg-amber-900/30' : ''}">
                      {#if fieldValues}
                        <select
                          class="bg-gray-800 border border-gray-700 rounded px-1.5 py-0.5 text-sm text-gray-100 text-right focus:outline-none focus:border-emerald-500 {edited ? 'border-amber-600' : ''}"
                          value={effective ?? ''}
                          onchange={(e) => handleCellInput(entryIdx, col.key, e.target.value)}
                        >
                          <option value="">{edited ? '(clear)' : '(none)'}</option>
                          {#each fieldValues as val}
                            <option value={val}>{val}</option>
                          {/each}
                        </select>
                      {:else if fieldType === 'number'}
                        <input
                          type="number"
                          step="0.01"
                          class="bg-gray-800 border border-gray-700 rounded px-1.5 py-0.5 text-sm text-gray-100 text-right w-24 focus:outline-none focus:border-emerald-500 {edited ? 'border-amber-600' : ''}"
                          value={effective ?? ''}
                          oninput={(e) => handleCellInput(entryIdx, col.key, e.target.value)}
                        />
                      {:else}
                        <input
                          type="text"
                          class="bg-gray-800 border border-gray-700 rounded px-1.5 py-0.5 text-sm text-gray-100 text-right w-24 focus:outline-none focus:border-emerald-500 {edited ? 'border-amber-600' : ''}"
                          value={effective ?? ''}
                          oninput={(e) => handleCellInput(entryIdx, col.key, e.target.value)}
                        />
                      {/if}
                    </td>
                  {:else}
                    <td class="text-right px-4 py-1.5 text-gray-300">{getOriginalValue(entry, col.key) || '—'}</td>
                  {/if}
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>
