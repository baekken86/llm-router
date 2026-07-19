<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<tbody class={cn("[&_tr:last-child]:border-0", className)} {...restProps}>
  {@render children?.()}
</tbody>
