<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<thead class={cn("[&_tr]:border-b", className)} {...restProps}>
  {@render children?.()}
</thead>
