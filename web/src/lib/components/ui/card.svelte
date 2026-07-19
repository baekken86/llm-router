<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<div
  class={cn("rounded-xl border border-border bg-card text-card-foreground shadow", className)}
  {...restProps}
>
  {@render children?.()}
</div>
