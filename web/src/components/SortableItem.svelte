<script>
  import { createSortable } from '@dnd-kit/svelte/sortable';

  let {
    id,
    index,
    group,
    data,
    children,
  } = $props();

  const sortable = createSortable({
    get id() { return id; },
    get index() { return index; },
    get group() { return group; },
    get data() { return data; },
  });
</script>

<div {@attach sortable.attach}>
  {@render children?.(sortable)}
</div>
