<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<div class={cn("text-2xl font-semibold leading-none tracking-tight", className)} {...restProps}>
  {@render children?.()}
</div>
