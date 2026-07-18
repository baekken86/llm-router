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
    id,
    index,
    group,
    data,
  });
</script>

<div {@attach sortable.attach}>
  {@render children?.(sortable)}
</div>
