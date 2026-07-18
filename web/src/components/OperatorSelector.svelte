<script>
  import { getOperatorsForType } from '../lib/api.js';

  let { fieldType = 'string', value = '', onChange } = $props();

  let operators = $derived(getOperatorsForType(fieldType));
</script>

<select
  {value}
  onchange={(e) => onChange(e.target.value)}
  class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500 min-w-[80px]"
>
  <option value="">Op...</option>
  {#each operators as op}
    <option value={op.op} title={op.description}>{op.label}</option>
  {/each}
</select>
