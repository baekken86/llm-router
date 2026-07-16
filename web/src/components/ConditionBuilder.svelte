<script>
  import { metadataFields } from '../lib/stores.js';
  import { getFieldType } from '../lib/fields.js';
  import FieldSelector from './FieldSelector.svelte';
  import OperatorSelector from './OperatorSelector.svelte';
  import ValueInput from './ValueInput.svelte';

  let { conditions = [], onchange } = $props();

  function addCondition() {
    onchange([...conditions, { key: '', op: '', value: '' }]);
  }

  function removeCondition(idx) {
    const next = conditions.filter((_, i) => i !== idx);
    onchange(next);
  }

  function updateCondition(idx, field, val) {
    const next = conditions.map((c, i) => {
      if (i !== idx) return c;
      const updated = { ...c, [field]: val };
      if (field === 'key') {
        updated.op = '';
        updated.value = '';
      }
      if (field === 'op' && val === 'in') {
        updated.value = [];
      }
      return updated;
    });
    onchange(next);
  }

  function getCondType(cond) {
    return getFieldType($metadataFields, cond.key);
  }

  function formatSummary() {
    return conditions
      .filter(c => c.key && c.op)
      .map(c => {
        const val = Array.isArray(c.value) ? `[${c.value.join(',')}]` : c.value;
        return `${c.key} ${c.op} ${val}`;
      })
      .join(' AND ');
  }
</script>

<div class="space-y-3">
  {#each conditions as cond, idx}
    <div class="flex items-center gap-2 flex-wrap">
      <FieldSelector
        value={cond.key}
        onchange={(v) => updateCondition(idx, 'key', v)}
      />
      <OperatorSelector
        fieldType={getCondType(cond)}
        value={cond.op}
        onchange={(v) => updateCondition(idx, 'op', v)}
      />
      <ValueInput
        fieldKey={cond.key}
        fieldType={getCondType(cond)}
        operator={cond.op}
        value={cond.value}
        onchange={(v) => updateCondition(idx, 'value', v)}
      />
      <button
        class="text-gray-500 hover:text-red-400 px-1"
        onclick={() => removeCondition(idx)}
        title="Remove condition"
      >
        x
      </button>
    </div>
  {/each}

  <button
    class="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
    onclick={addCondition}
  >
    + Add condition
  </button>

  {#if formatSummary()}
    <p class="text-xs text-gray-500 mt-2">
      Preview: <span class="text-gray-400">{formatSummary()}</span>
    </p>
  {/if}
</div>
