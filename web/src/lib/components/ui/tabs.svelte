<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<div class={cn("flex-1", className)} {...restProps}>
  {@render children?.()}
</div>
