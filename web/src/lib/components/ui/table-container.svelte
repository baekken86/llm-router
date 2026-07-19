<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<div class={cn("relative w-full overflow-auto", className)} {...restProps}>
  {@render children?.()}
</div>
