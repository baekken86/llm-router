<script>
  import { Select as SelectPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<SelectPrimitive.Label
  class={cn("px-2 py-1.5 text-sm font-semibold text-foreground", className)}
  {...restProps}
/>
