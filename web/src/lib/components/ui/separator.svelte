<script>
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<div
  role="none"
  class={cn("shrink-0 bg-border h-[1px] w-full", className)}
  {...restProps}
></div>
