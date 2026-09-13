<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast, filterConditions } from '../lib/stores.js';
  import ConditionBuilder from './ConditionBuilder.svelte';
  import Switch from '../lib/components/ui/switch.svelte';
  import ResolvedPreview from './ResolvedPreview.svelte';

  let conditions = $state([]);
  let loading = $state(true);
  let filter = $state('');
  let deleteConfirmId = $state(null);
  let editingId = $state(null); // null = closed, 'new' = create, number = edit
  let saving = $state(false);

  let formName = $state('');
  let formDescription = $state('');
  let formEnabled = $state(true);
  let formFilterExpr = $state({ and: [{ key: '', op: 'eq', value: '' }] });

  const sorted = $derived.by(() =>
    [...conditions].sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
  );

  const filtered = $derived.by(() => {
    if (!filter) return sorted;
    const q = filter.toLowerCase();
    return sorted.filter(c =>
      (c.name || '').toLowerCase().includes(q) ||
      (c.description || '').toLowerCase().includes(q)
    );
  });

  const previewFilterExpr = $derived.by(() => {
    if (editingId !== null) {
      return formFilterExpr || null;
    }
    const nodes = sorted
      .filter(c => c.enabled !== false)
      .map(c => c.filter_expr)
      .filter(Boolean);
    if (nodes.length === 0) return null;
    if (nodes.length === 1) return nodes[0];
    return { and: nodes };
  });

  let previewOpen = $state(true);

  async function load() {
    loading = true;
    try {
      conditions = await apiFetch('/api/v1/global-filter-conditions');
      filterConditions.set(conditions);
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    editingId = 'new';
    formName = '';
    formDescription = '';
    formEnabled = true;
    formFilterExpr = { and: [{ key: '', op: 'eq', value: '' }] };
  }

  function openEdit(c) {
    editingId = c.id;
    formName = c.name || '';
    formDescription = c.description || '';
    formEnabled = c.enabled !== false;
    formFilterExpr = c.filter_expr || { and: [] };
  }

  function closeForm() {
    editingId = null;
  }

  function filterSummary(node, depth = 0) {
    if (!node) return '—';
    if (node.key) {
      const val = Array.isArray(node.value) ? `[${node.value.join(',')}]` : node.value;
      return `${node.key} ${node.op} ${val}`;
    }
    if (node.not) {
      return `NOT (${filterSummary(node.not, depth + 1)})`;
    }
    const op = node.and ? 'AND' : 'OR';
    const items = node.and || node.or || [];
    if (items.length === 0) return depth === 0 ? '—' : '';
    const parts = items.map(i => filterSummary(i, depth + 1)).filter(Boolean);
    if (parts.length === 0) return depth === 0 ? '—' : '';
    if (parts.length === 1 && parts[0]) return parts[0];
    return parts.map(p => depth > 0 && p.includes(' ') ? `(${p})` : p).join(` ${op} `);
  }

  function nextPosition() {
    const used = conditions.map(c => c.position ?? 0);
    let p = 0;
    while (used.includes(p)) p++;
    return p;
  }

  async function toggleEnabled(c, checked) {
    const prev = conditions;
    conditions = conditions.map(x => x.id === c.id ? { ...x, enabled: checked } : x);
    try {
      await apiFetch(`/api/v1/global-filter-conditions/${c.id}`, {
        method: 'PUT',
        body: {
          name: c.name,
          description: c.description || '',
          filter_expr: c.filter_expr,
          enabled: checked,
          position: c.position ?? 0,
        },
      });
      addToast(checked ? 'Condition enabled' : 'Condition disabled', 'success');
      filterConditions.set(conditions);
    } catch (e) {
      conditions = prev;
      addToast(e.message, 'error');
    }
  }

  async function save() {
    if (!formName.trim()) {
      addToast('Name is required', 'error');
      return;
    }
    saving = true;
    const body = {
      name: formName.trim(),
      description: formDescription.trim(),
      filter_expr: formFilterExpr,
      enabled: formEnabled,
      position: editingId === 'new' ? nextPosition() : (sorted.find(c => c.id === editingId)?.position ?? 0),
    };
    try {
      if (editingId === 'new') {
        await apiFetch('/api/v1/global-filter-conditions', { method: 'POST', body });
        addToast('Created', 'success');
      } else {
        await apiFetch(`/api/v1/global-filter-conditions/${editingId}`, { method: 'PUT', body });
        addToast('Updated', 'success');
      }
      closeForm();
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function moveRow(fromIdx, dir) {
    const toIdx = fromIdx + dir;
    if (toIdx < 0 || toIdx >= filtered.length) return;
    const prev = conditions;
    const next = [...sorted];
    const a = next.indexOf(filtered[fromIdx]);
    const b = next.indexOf(filtered[toIdx]);
    next[a] = filtered[toIdx];
    next[b] = filtered[fromIdx];
    conditions = next;
    try {
      conditions = await apiFetch('/api/v1/global-filter-conditions/reorder', {
        method: 'PUT',
        body: { ids: next.map(c => c.id) },
      });
      filterConditions.set(conditions);
    } catch (e) {
      conditions = prev;
      addToast(e.message, 'error');
    }
  }

  async function deleteCondition(id) {
    try {
      await apiFetch(`/api/v1/global-filter-conditions/${id}`, { method: 'DELETE' });
      addToast('Condition deleted', 'success');
      deleteConfirmId = null;
      await load();
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  onMount(() => { load(); });
</script>

<div>
  <div class="flex items-center justify-between mb-6">
    <h2 class="text-xl font-bold text-gray-100">Filter Conditions</h2>
    <button
      class="text-xs text-gray-500 hover:text-gray-300"
      onclick={() => previewOpen = !previewOpen}
    >
      {previewOpen ? '▾' : '▸'} Preview (enabled conditions)
    </button>
  </div>
  <div class="flex items-center gap-4 mb-6">
    <input
      type="text"
      placeholder="Filter by name..."
      bind:value={filter}
      class="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-emerald-500"
    />
    <button
      class="bg-emerald-600 hover:bg-emerald-700 text-white rounded px-4 py-2 text-sm font-medium"
      onclick={openCreate}
    >
      + Add Condition
    </button>
  </div>

  {#if editingId !== null}
    <div class="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-6">
      <h3 class="text-sm font-medium text-gray-300 mb-4">
        {editingId === 'new' ? 'New Condition' : 'Edit Condition'}
      </h3>
      <form onsubmit={(e) => { e.preventDefault(); save(); }} class="space-y-4 max-w-3xl">
        <div>
          <label class="block text-sm text-gray-400 mb-1">Name</label>
          <input
            type="text"
            bind:value={formName}
            placeholder="e.g. free-only, multimodal, exclude-deprecated"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
          />
        </div>
        <div>
          <label class="block text-sm text-gray-400 mb-1">Description</label>
          <textarea
            bind:value={formDescription}
            placeholder="What does this filter condition do?"
            rows="2"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
          ></textarea>
        </div>
        <div>
          <label class="block text-sm text-gray-400 mb-2">Filter Expression</label>
          <ConditionBuilder node={formFilterExpr} onChange={(v) => formFilterExpr = v} />
        </div>
        <div class="flex items-center gap-3">
          <Switch
            checked={formEnabled}
            onCheckedChange={(c) => formEnabled = c}
            class="data-[state=checked]:bg-emerald-600 data-[state=unchecked]:bg-gray-700"
          />
          <span class="text-sm text-gray-400">Enabled</span>
        </div>
        <div class="flex gap-3">
          <button
            type="submit"
            disabled={saving}
            class="bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white rounded px-6 py-2 text-sm font-medium"
          >
            {saving ? 'Saving...' : editingId === 'new' ? 'Create' : 'Update'}
          </button>
          <button
            type="button"
            class="text-sm text-gray-400 hover:text-white px-4 py-2"
            onclick={closeForm}
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  {/if}

  {#if loading}
    <p class="text-gray-500">Loading...</p>
  {:else if filtered.length === 0}
    <p class="text-gray-500">No filter conditions defined.</p>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-gray-500 border-b border-gray-800">
            <th class="text-left px-4 py-3">Enabled</th>
            <th class="text-left px-4 py-3">Name</th>
            <th class="text-left px-4 py-3">Description</th>
            <th class="text-center px-4 py-3">Position</th>
            <th class="text-center px-4 py-3"></th>
            <th class="text-left px-4 py-3">Filter</th>
            <th class="text-right px-4 py-3">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as c, i (c.id)}
            <tr class="border-b border-gray-850 hover:bg-gray-850/50">
              <td class="px-4 py-2.5">
                <Switch
                  checked={c.enabled !== false}
                  onCheckedChange={(v) => toggleEnabled(c, v)}
                  class="data-[state=checked]:bg-emerald-600 data-[state=unchecked]:bg-gray-700"
                />
              </td>
              <td class="px-4 py-2.5 text-emerald-400 font-mono">{c.name}</td>
              <td class="px-4 py-2.5 text-gray-300">{c.description}</td>
              <td class="px-4 py-2.5 text-center text-gray-500">{c.position ?? 0}</td>
              <td class="px-4 py-2.5 text-center whitespace-nowrap">
                <button
                  class="text-xs text-gray-500 hover:text-white px-1 {i === 0 ? 'invisible' : ''}"
                  title="Move up (applied earlier)"
                  onclick={() => moveRow(i, -1)}
                >▲</button>
                <button
                  class="text-xs text-gray-500 hover:text-white px-1 {i === filtered.length - 1 ? 'invisible' : ''}"
                  title="Move down (applied later)"
                  onclick={() => moveRow(i, 1)}
                >▼</button>
              </td>
              <td class="px-4 py-2.5 text-gray-400 text-xs font-mono max-w-md truncate" title={filterSummary(c.filter_expr)}>
                {filterSummary(c.filter_expr)}
              </td>
              <td class="px-4 py-2.5 text-right">
                <button
                  class="text-xs text-gray-500 hover:text-emerald-400 mr-2"
                  onclick={() => openEdit(c)}
                >Edit</button>
                {#if deleteConfirmId === c.id}
                  <span class="text-xs text-gray-400 mr-2">Delete?</span>
                  <button
                    class="text-xs text-red-400 hover:text-red-300 mr-2"
                    onclick={() => deleteCondition(c.id)}
                  >Yes</button>
                  <button
                    class="text-xs text-gray-500 hover:text-gray-300"
                    onclick={() => deleteConfirmId = null}
                  >No</button>
                {:else}
                  <button
                    class="text-xs text-gray-500 hover:text-red-400"
                    onclick={() => deleteConfirmId = c.id}
                  >Delete</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  {#if previewOpen}
    <div class="mt-8 bg-gray-900 border border-gray-800 rounded-lg p-4">
      <h3 class="text-sm font-medium text-gray-300 mb-2">
        Preview <span class="text-gray-500 text-xs">(enabled conditions — per-VM overrides may disable some of these)</span>
      </h3>
      {#if previewFilterExpr}
        <ResolvedPreview
          previewMode={true}
          composition={{ filter_expr: previewFilterExpr, sort_expr: [] }}
        />
      {:else}
        <p class="text-sm text-gray-500">No enabled filter conditions.</p>
      {/if}
    </div>
  {/if}
</div>
