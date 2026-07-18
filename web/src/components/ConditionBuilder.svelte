<script>
  import { metadataFields } from '../lib/stores.js';
  import { getFieldType } from '../lib/fields.js';
  import FieldSelector from './FieldSelector.svelte';
  import OperatorSelector from './OperatorSelector.svelte';
  import ValueInput from './ValueInput.svelte';
  import ConditionBuilder from './ConditionBuilder.svelte';

  let { node = { and: [] }, onChange, depth = 0 } = $props();

  let isGroup = $derived(node && !node.key && (node.and || node.or));
  let mode = $derived(node?.and ? 'and' : 'or');

  function getItems(n) {
    if (n?.and) return n.and;
    if (n?.or) return n.or;
    return [];
  }

  function setItems(n, items) {
    if (n?.and) return { and: items };
    if (n?.or) return { or: items };
    return n;
  }

  function toggleMode() {
    const newMode = mode === 'and' ? 'or' : 'and';
    const items = getItems(node);
    if (newMode === 'and') {
      onChange({ and: items });
    } else {
      onChange({ or: items });
    }
  }

  function addItem(type) {
    const items = getItems(node);
    if (type === 'condition') {
      onChange(setItems(node, [...items, { key: '', op: '', value: '' }]));
    } else {
      onChange(setItems(node, [...items, { and: [] }]));
    }
  }

  function removeItem(idx) {
    const items = getItems(node).filter((_, i) => i !== idx);
    onChange(setItems(node, items));
  }

  function updateItem(idx, newNode) {
    const items = getItems(node).map((item, i) => i === idx ? newNode : item);
    onChange(setItems(node, items));
  }

  function toggleNot(idx) {
    const items = getItems(node);
    const item = items[idx];
    if (item?.not) {
      updateItem(idx, item.not);
    } else {
      updateItem(idx, { not: item });
    }
  }

  function isNegated(item) {
    return item?.not !== undefined;
  }

  function unwrapNot(item) {
    return item?.not ?? item;
  }

  function getCondType(cond) {
    const inner = unwrapNot(cond);
    return getFieldType($metadataFields, inner.key);
  }

  function formatSummary(n) {
    if (!n) return '';
    if (n.key) {
      const neg = isNegated(n) ? 'NOT ' : '';
      const inner = unwrapNot(n);
      const val = Array.isArray(inner.value) ? `[${inner.value.join(',')}]` : inner.value;
      return `${neg}${inner.key} ${inner.op} ${val}`;
    }
    const op = n.and ? 'AND' : 'OR';
    const items = getItems(n);
    const parts = items.map(i => formatSummary(i)).filter(Boolean);
    if (parts.length === 0) return '';
    if (parts.length === 1) return parts[0];
    return parts.map(p => parts.length > 1 && p.includes(' ') ? `(${p})` : p).join(` ${op} `);
  }

  const indentClass = [
    'border-l-2 border-gray-700 pl-3',
    'border-l-2 border-emerald-800 pl-3',
    'border-l-2 border-blue-800 pl-3',
    'border-l-2 border-purple-800 pl-3',
    'border-l-2 border-amber-800 pl-3',
  ];
</script>

{#if isGroup}
  <div class="space-y-2 {depth > 0 ? indentClass[depth % indentClass.length] + ' mt-2' : ''}">
    <div class="flex items-center gap-2 mb-2">
      <button
        class="px-2 py-0.5 text-xs font-mono rounded {mode === 'and' ? 'bg-emerald-600 text-white' : 'bg-blue-600 text-white'}"
        onclick={toggleMode}
        title="Toggle AND/OR"
      >
        {mode === 'and' ? 'AND' : 'OR'}
      </button>
      {#if depth > 0}
        <button
          class="text-gray-500 hover:text-red-400 text-xs px-1"
          onclick={() => onChange(null)}
          title="Remove group"
        >
          x
        </button>
      {/if}
    </div>

    {#each getItems(node) as item, idx (idx)}
      {#if item?.key !== undefined || item?.not?.key !== undefined}
        <div class="flex items-center gap-2 flex-wrap">
          <button
            class="text-xs px-1.5 py-0.5 rounded {isNegated(item) ? 'bg-red-600 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'}"
            onclick={() => toggleNot(idx)}
            title="Toggle NOT"
          >
            NOT
          </button>
          <FieldSelector
            value={unwrapNot(item).key}
            onChange={(v) => {
              const inner = unwrapNot(item);
              const updated = { ...inner, key: v, op: '', value: '' };
              updateItem(idx, isNegated(item) ? { not: updated } : updated);
            }}
          />
          <OperatorSelector
            fieldType={getCondType(item)}
            value={unwrapNot(item).op}
            onChange={(v) => {
              const inner = unwrapNot(item);
              const updated = { ...inner, op: v };
              if (v === 'in') updated.value = [];
              updateItem(idx, isNegated(item) ? { not: updated } : updated);
            }}
          />
          <ValueInput
            fieldKey={unwrapNot(item).key}
            fieldType={getCondType(item)}
            operator={unwrapNot(item).op}
            value={unwrapNot(item).value}
            onChange={(v) => {
              const inner = unwrapNot(item);
              updateItem(idx, isNegated(item) ? { not: { ...inner, value: v } } : { ...inner, value: v });
            }}
          />
          <button
            class="text-gray-500 hover:text-red-400 px-1"
            onclick={() => removeItem(idx)}
            title="Remove condition"
          >
            x
          </button>
        </div>
      {:else}
        <ConditionBuilder node={item} depth={depth + 1} onChange={(v) => {
          if (v === null) removeItem(idx);
          else updateItem(idx, v);
        }} />
      {/if}
    {/each}

    <div class="flex gap-2">
      <button
        class="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
        onclick={() => addItem('condition')}
      >
        + Condition
      </button>
      <button
        class="text-sm text-blue-400 hover:text-blue-300 flex items-center gap-1"
        onclick={() => addItem('group')}
      >
        + Group
      </button>
    </div>
  </div>
{:else}
  <div class="flex items-center gap-2 flex-wrap {depth > 0 ? indentClass[depth % indentClass.length] + ' mt-2' : ''}">
    <FieldSelector
      value={node?.key || ''}
      onChange={(v) => onChange({ key: v, op: '', value: '' })}
    />
    <OperatorSelector
      fieldType={getFieldType($metadataFields, node?.key)}
      value={node?.op || ''}
      onChange={(v) => {
        const updated = { ...node, op: v };
        if (v === 'in') updated.value = [];
        onChange(updated);
      }}
    />
    <ValueInput
      fieldKey={node?.key}
      fieldType={getFieldType($metadataFields, node?.key)}
      operator={node?.op}
      value={node?.value}
      onChange={(v) => onChange({ ...node, value: v })}
    />
  </div>
{/if}

{#if isGroup && formatSummary(node)}
  <p class="text-xs text-gray-500 mt-2 {depth > 0 ? 'ml-4' : ''}">
    Preview: <span class="text-gray-400">{formatSummary(node)}</span>
  </p>
{/if}
