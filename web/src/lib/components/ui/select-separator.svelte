<script>
  import { Select as SelectPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<SelectPrimitive.Separator
  class={cn("-mx-1 my-1 h-px bg-muted", className)}
  {...restProps}
/>
