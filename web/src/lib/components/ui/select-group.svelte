<script>
  import { Select as SelectPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<SelectPrimitive.Group class={cn(className)} {...restProps} />
