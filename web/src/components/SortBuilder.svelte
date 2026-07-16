<script>
  import { metadataFields } from '../lib/stores.js';
  import { getFieldType, getFieldValues } from '../lib/fields.js';
  import FieldSelector from './FieldSelector.svelte';

  let { criteria = [], onchange } = $props();

  function addCriterion() {
    onchange([...criteria, { key: '', direction: 'desc' }]);
  }

  function removeCriterion(idx) {
    onchange(criteria.filter((_, i) => i !== idx));
  }

  function updateCriterion(idx, field, val) {
    const next = criteria.map((c, i) => {
      if (i !== idx) return c;
      return { ...c, [field]: val };
    });
    onchange(next);
  }

  function toggleMode(idx) {
    const c = criteria[idx];
    if (c.direction !== undefined) {
      const vals = getFieldValues($metadataFields, c.key);
      updateCriterion(idx, 'order', vals || []);
      const next = criteria.map((cr, i) => {
        if (i !== idx) return cr;
        const { direction, ...rest } = cr;
        return rest;
      });
      onchange(next);
    } else {
      updateCriterion(idx, 'direction', 'desc');
      const next = criteria.map((cr, i) => {
        if (i !== idx) return cr;
        const { order, ...rest } = cr;
        return rest;
      });
      onchange(next);
    }
  }

  function moveUp(idx) {
    if (idx === 0) return;
    const next = [...criteria];
    [next[idx - 1], next[idx]] = [next[idx], next[idx - 1]];
    onchange(next);
  }

  function moveDown(idx) {
    if (idx >= criteria.length - 1) return;
    const next = [...criteria];
    [next[idx], next[idx + 1]] = [next[idx + 1], next[idx]];
    onchange(next);
  }

  function formatSummary() {
    return criteria
      .filter(c => c.key)
      .map(c => {
        if (c.direction) return `${c.key} ${c.direction}`;
        if (c.order) return `${c.key}: [${c.order.join(', ')}]`;
        return c.key;
      })
      .join(', ');
  }
</script>

<div class="space-y-3">
  {#each criteria as crit, idx}
    <div class="flex items-center gap-2 flex-wrap">
      <FieldSelector
        value={crit.key}
        onchange={(v) => updateCriterion(idx, 'key', v)}
      />

      {#if crit.direction !== undefined}
        <select
          value={crit.direction}
          onchange={(e) => updateCriterion(idx, 'direction', e.target.value)}
          class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
        >
          <option value="asc">Ascending</option>
          <option value="desc">Descending</option>
        </select>
      {:else if crit.order}
        <div class="flex flex-wrap gap-1">
          {#each crit.order as v, i}
            <span class="px-2 py-1 text-xs bg-gray-700 text-gray-300 rounded">{v}</span>
          {/each}
        </div>
      {/if}

      <button
        class="text-xs text-gray-500 hover:text-gray-300 px-1"
        onclick={() => toggleMode(idx)}
        title="Toggle direction/custom order"
      >
        {crit.direction !== undefined ? '[]' : '↕'}
      </button>

      <button
        class="text-xs text-gray-500 hover:text-gray-300 px-1"
        onclick={() => moveUp(idx)}
        disabled={idx === 0}
      >
        ^
      </button>
      <button
        class="text-xs text-gray-500 hover:text-gray-300 px-1"
        onclick={() => moveDown(idx)}
        disabled={idx >= criteria.length - 1}
      >
        v
      </button>

      <button
        class="text-gray-500 hover:text-red-400 px-1"
        onclick={() => removeCriterion(idx)}
        title="Remove sort criterion"
      >
        x
      </button>
    </div>
  {/each}

  <button
    class="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
    onclick={addCriterion}
  >
    + Add sort criterion
  </button>

  {#if formatSummary()}
    <p class="text-xs text-gray-500 mt-2">
      Preview: <span class="text-gray-400">{formatSummary()}</span>
    </p>
  {/if}
</div>
