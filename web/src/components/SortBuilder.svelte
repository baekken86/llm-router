<script>
  import { metadataFields } from '../lib/stores.js';
  import { getFieldType, getFieldValues } from '../lib/fields.js';
  import FieldSelector from './FieldSelector.svelte';
  import OperatorSelector from './OperatorSelector.svelte';
  import ValueInput from './ValueInput.svelte';

  let { criteria = [], onchange } = $props();

  function addSort() {
    onchange([...criteria, { key: '', direction: 'desc' }]);
  }

  function addCondition() {
    onchange([...criteria, { condition: { key: '', op: 'eq', value: '' } }]);
  }

  function removeEntry(idx) {
    onchange(criteria.filter((_, i) => i !== idx));
  }

  function updateSort(idx, field, val) {
    const next = criteria.map((c, i) => {
      if (i !== idx) return c;
      return { ...c, [field]: val };
    });
    onchange(next);
  }

  function updateConditionField(idx, field, val) {
    const next = criteria.map((c, i) => {
      if (i !== idx) return c;
      const cond = { ...(c.condition || { key: '', op: 'eq', value: '' }), [field]: val };
      if (field === 'key') {
        cond.op = '';
        cond.value = '';
      }
      return { ...c, condition: cond };
    });
    onchange(next);
  }

  function toggleSortMode(idx) {
    const c = criteria[idx];
    if (c.direction !== undefined) {
      const vals = getFieldValues($metadataFields, c.key);
      updateSort(idx, 'order', vals || []);
      const next = criteria.map((cr, i) => {
        if (i !== idx) return cr;
        const { direction, ...rest } = cr;
        return rest;
      });
      onchange(next);
    } else {
      updateSort(idx, 'direction', 'desc');
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
      .map(c => {
        if (c.condition) {
          const val = Array.isArray(c.condition.value) ? `[${c.condition.value.join(',')}]` : c.condition.value;
          return `IF ${c.condition.key} ${c.condition.op} ${val} ${c.direction || ''}`.trim();
        }
        if (c.key) {
          if (c.direction) return `${c.key} ${c.direction}`;
          if (c.order) return `${c.key}: [${c.order.join(', ')}]`;
          return c.key;
        }
        return '';
      })
      .filter(Boolean)
      .join(', ');
  }
</script>

<div class="space-y-3">
  {#each criteria as entry, idx}
    {#if entry.condition}
      <div class="flex items-center gap-2 flex-wrap border-l-2 border-amber-700 pl-3">
        <span class="text-xs text-amber-400 font-mono">IF</span>
        <FieldSelector
          value={entry.condition.key}
          onchange={(v) => updateConditionField(idx, 'key', v)}
        />
        <OperatorSelector
          fieldType={getFieldType($metadataFields, entry.condition.key)}
          value={entry.condition.op}
          onchange={(v) => updateConditionField(idx, 'op', v)}
        />
        <ValueInput
          fieldKey={entry.condition.key}
          fieldType={getFieldType($metadataFields, entry.condition.key)}
          operator={entry.condition.op}
          value={entry.condition.value}
          onchange={(v) => updateConditionField(idx, 'value', v)}
        />
        <select
          value={entry.direction || 'asc'}
          onchange={(e) => updateSort(idx, 'direction', e.target.value)}
          class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
        >
          <option value="asc">true first</option>
          <option value="desc">false first</option>
        </select>
        <button
          class="text-xs text-gray-500 hover:text-gray-300 px-1"
          onclick={() => moveUp(idx)}
          disabled={idx === 0}
        >^</button>
        <button
          class="text-xs text-gray-500 hover:text-gray-300 px-1"
          onclick={() => moveDown(idx)}
          disabled={idx >= criteria.length - 1}
        >v</button>
        <button
          class="text-gray-500 hover:text-red-400 px-1"
          onclick={() => removeEntry(idx)}
        >x</button>
      </div>
    {:else}
      <div class="flex items-center gap-2 flex-wrap">
        <FieldSelector
          value={entry.key || ''}
          onchange={(v) => updateSort(idx, 'key', v)}
        />

        {#if entry.direction !== undefined}
          <select
            value={entry.direction}
            onchange={(e) => updateSort(idx, 'direction', e.target.value)}
            class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
          >
            <option value="asc">Ascending</option>
            <option value="desc">Descending</option>
          </select>
        {:else if entry.order}
          <div class="flex flex-wrap gap-1">
            {#each entry.order as v}
              <span class="px-2 py-1 text-xs bg-gray-700 text-gray-300 rounded">{v}</span>
            {/each}
          </div>
        {/if}

        <button
          class="text-xs text-gray-500 hover:text-gray-300 px-1"
          onclick={() => toggleSortMode(idx)}
          title="Toggle direction/custom order"
        >
          {entry.direction !== undefined ? '[]' : '↕'}
        </button>
        <button
          class="text-xs text-gray-500 hover:text-gray-300 px-1"
          onclick={() => moveUp(idx)}
          disabled={idx === 0}
        >^</button>
        <button
          class="text-xs text-gray-500 hover:text-gray-300 px-1"
          onclick={() => moveDown(idx)}
          disabled={idx >= criteria.length - 1}
        >v</button>
        <button
          class="text-gray-500 hover:text-red-400 px-1"
          onclick={() => removeEntry(idx)}
        >x</button>
      </div>
    {/if}
  {/each}

  <div class="flex gap-2">
    <button
      class="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
      onclick={addSort}
    >
      + Sort
    </button>
    <button
      class="text-sm text-amber-400 hover:text-amber-300 flex items-center gap-1"
      onclick={addCondition}
    >
      + IF condition
    </button>
  </div>

  {#if formatSummary()}
    <p class="text-xs text-gray-500 mt-2">
      Preview: <span class="text-gray-400">{formatSummary()}</span>
    </p>
  {/if}
</div>
