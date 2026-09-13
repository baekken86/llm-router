<script>
  import { metadataFields } from '../lib/stores.js';
  import { getFieldType, getFieldValues } from '../lib/fields.js';
  import FieldSelector from './FieldSelector.svelte';
  import OperatorSelector from './OperatorSelector.svelte';
  import ValueInput from './ValueInput.svelte';
  import DragHandle from './DragHandle.svelte';
  import SortableItem from './SortableItem.svelte';
  import SortableTree from './SortableTree.svelte';
  import ConditionBuilder from './ConditionBuilder.svelte';

  let { criteria = [], onChange } = $props();

  function addSort() {
    onChange([...criteria, { key: '', direction: 'desc' }]);
  }

  function addCondition() {
    onChange([...criteria, { condition: { and: [{ key: '', op: 'eq', value: '' }] } }]);
  }

  function removeEntry(idx) {
    onChange(criteria.filter((_, i) => i !== idx));
  }

  function updateSort(idx, field, val) {
    const next = criteria.map((c, i) => {
      if (i !== idx) return c;
      return { ...c, [field]: val };
    });
    onChange(next);
  }

  function updateCondition(idx, newCondition) {
    if (newCondition === null) {
      removeEntry(idx);
      return;
    }
    const next = criteria.map((c, i) => {
      if (i !== idx) return c;
      return { ...c, condition: newCondition };
    });
    onChange(next);
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
      onChange(next);
    } else {
      updateSort(idx, 'direction', 'desc');
      const next = criteria.map((cr, i) => {
        if (i !== idx) return cr;
        const { order, ...rest } = cr;
        return rest;
      });
      onChange(next);
    }
  }

  function formatConditionSummary(n) {
    if (!n) return '';
    if (n.key) {
      const val = Array.isArray(n.value) ? `[${n.value.join(',')}]` : n.value;
      return `${n.key} ${n.op} ${val}`;
    }
    const op = n.and ? 'AND' : 'OR';
    const items = n.and || n.or || [];
    const parts = items.map(i => formatConditionSummary(i)).filter(Boolean);
    if (parts.length === 0) return '';
    if (parts.length === 1) return parts[0];
    return parts.map(p => parts.length > 1 && p.includes(' ') ? `(${p})` : p).join(` ${op} `);
  }

  function formatSummary() {
    return criteria
      .map(c => {
        if (c.condition) {
          return `IF ${formatConditionSummary(c.condition)} ${c.direction || ''}`.trim();
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

  function handleDragEnd(event) {
    const { operation } = event;
    const source = operation.source;
    const target = operation.target;

    if (!source || !target) return;

    // SortableDraggable/SortableDroppable have .index at runtime
    // even though base Draggable/Droppable types don't expose it
    const fromIndex = /** @type {any} */ (source).index;
    const toIndex = /** @type {any} */ (target).index;

    if (fromIndex === toIndex || fromIndex === undefined || toIndex === undefined) return;

    const next = [...criteria];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    onChange(next);
  }
</script>

<SortableTree onDragEnd={handleDragEnd}>
  <div class="space-y-3">
    {#each criteria as entry, idx (idx)}
      <SortableItem
        id={`sort-${idx}`}
        index={idx}
        group="sort-list"
        data={{ type: 'sort-entry', index: idx }}
      >
        {#snippet children(sortable)}
          {#if entry.condition}
            <div class="flex items-start gap-2 flex-wrap border-l-2 border-amber-700 pl-3 pt-1">
              {#if criteria.length > 1}
                <DragHandle attachHandle={sortable.attachHandle} />
              {/if}
              <span class="text-xs text-amber-400 font-mono mt-2">IF</span>
              <div class="flex-1 min-w-0">
                <ConditionBuilder
                  node={entry.condition}
                  depth={0}
                  groupKey={`condition-${idx}`}
                  onChange={(v) => updateCondition(idx, v)}
                />
              </div>
              <div class="flex items-center gap-2 mt-2">
                <select
                  value={entry.direction || 'asc'}
                  onchange={(e) => updateSort(idx, 'direction', e.target.value)}
                  class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
                >
                  <option value="asc">true first</option>
                  <option value="desc">false first</option>
                </select>
                <button
                  type="button"
                  class="text-gray-500 hover:text-red-400 px-1"
                  onclick={() => removeEntry(idx)}
                >x</button>
              </div>
            </div>
          {:else}
            <div class="flex items-center gap-2 flex-wrap">
              {#if criteria.length > 1}
                <DragHandle attachHandle={sortable.attachHandle} />
              {/if}
              <FieldSelector
                value={entry.key || ''}
                onChange={(v) => updateSort(idx, 'key', v)}
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
                type="button"
                class="text-xs text-gray-500 hover:text-gray-300 px-1"
                onclick={() => toggleSortMode(idx)}
                title="Toggle direction/custom order"
              >
                {entry.direction !== undefined ? '[]' : '↕'}
              </button>
              <button
                type="button"
                class="text-gray-500 hover:text-red-400 px-1"
                onclick={() => removeEntry(idx)}
              >x</button>
            </div>
          {/if}
        {/snippet}
      </SortableItem>
    {/each}

    <div class="flex gap-2">
      <button
        type="button"
        class="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
        onclick={addSort}
      >
        + Sort
      </button>
      <button
        type="button"
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
</SortableTree>
