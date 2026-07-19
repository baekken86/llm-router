<script>
  import { cn } from "$lib/utils.js";

  let { class: className, inset, children, ...restProps } = $props();
</script>

<div
  class={cn("px-2 py-1.5 text-sm font-semibold text-foreground", inset && "pl-8", className)}
  {...restProps}
>
  {@render children?.()}
</div>
