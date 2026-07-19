<script>
  import { ContextMenu as ContextMenuPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, children, ...restProps } = $props();
</script>

<ContextMenuPrimitive.Trigger
  class={cn(className)}
  {...restProps}
>
  {@render children?.()}
</ContextMenuPrimitive.Trigger>
