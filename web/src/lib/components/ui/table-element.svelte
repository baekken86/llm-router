<script>
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<table class={cn("w-full caption-bottom text-sm", className)} {...restProps}>
  {@render children?.()}
</table>
