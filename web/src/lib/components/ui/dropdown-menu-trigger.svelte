<script>
  import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<DropdownMenuPrimitive.Trigger
  class={cn(className)}
  {...restProps}
>
  {@render children?.()}
</DropdownMenuPrimitive.Trigger>
