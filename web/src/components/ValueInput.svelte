<script>
  import { getFieldValues, getFieldMinMax } from '../lib/fields.js';
  import { metadataFields } from '../lib/stores.js';

  let { fieldKey = '', fieldType = 'string', operator = '', value = '', onchange } = $props();

  let knownValues = $derived(getFieldValues($metadataFields, fieldKey));
  let minMax = $derived(getFieldMinMax($metadataFields, fieldKey));
  let selectedValues = $derived(
    operator === 'in' && Array.isArray(value) ? value : []
  );

  function handleInput(e) {
    const v = e.target.value;
    if (fieldType === 'number') {
      onchange(v === '' ? '' : Number(v));
    } else {
      onchange(v);
    }
  }

  function handleToggle(val) {
    onchange(val === 'true');
  }

  function toggleInValue(val) {
    let arr = Array.isArray(value) ? [...value] : [];
    const idx = arr.indexOf(val);
    if (idx >= 0) arr.splice(idx, 1);
    else arr.push(val);
    onchange(arr);
  }
</script>

{#if operator === 'in' && knownValues}
  <div class="flex flex-wrap gap-1">
    {#each knownValues as v}
      <button
        class="px-2 py-1 text-xs rounded {selectedValues.includes(v) ? 'bg-emerald-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
        onclick={() => toggleInValue(v)}
      >
        {v}
      </button>
    {/each}
  </div>
{:else if fieldType === 'boolean'}
  <div class="flex gap-2">
    <button
      class="px-3 py-1 text-xs rounded {value === true ? 'bg-emerald-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
      onclick={() => handleToggle('true')}
    >
      true
    </button>
    <button
      class="px-3 py-1 text-xs rounded {value === false ? 'bg-emerald-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
      onclick={() => handleToggle('false')}
    >
      false
    </button>
  </div>
{:else if fieldType === 'number'}
  <div class="flex items-center gap-2">
    <input
      type="number"
      {value}
      min={minMax?.min ?? 0}
      max={minMax?.max ?? 100}
      step="any"
      oninput={handleInput}
      class="w-24 bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
    />
    {#if minMax}
      <input
        type="range"
        {value}
        min={minMax.min}
        max={minMax.max}
        step="1"
        oninput={handleInput}
        class="flex-1 accent-emerald-500"
      />
    {/if}
  </div>
{:else if knownValues}
  <select
    {value}
    onchange={(e) => onchange(e.target.value)}
    class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
  >
    <option value="">Select...</option>
    {#each knownValues as v}
      <option value={v}>{v}</option>
    {/each}
  </select>
{:else}
  <input
    type="text"
    {value}
    oninput={handleInput}
    placeholder="Value..."
    class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500"
  />
{/if}
