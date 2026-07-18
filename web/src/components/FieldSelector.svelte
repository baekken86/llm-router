<script>
  import { getFieldGroups } from '../lib/fields.js';
  import { metadataFields } from '../lib/stores.js';

  let { value = '', onChange } = $props();

  let groups = $derived(getFieldGroups($metadataFields));
</script>

<select
  {value}
  onchange={(e) => onChange(e.target.value)}
  class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-emerald-500 min-w-[160px]"
>
  <option value="">Select field...</option>
  {#each groups as group}
    <optgroup label={group.label}>
      {#each group.fields as field}
        <option value={field.key}>{field.key}</option>
      {/each}
    </optgroup>
  {/each}
</select>
