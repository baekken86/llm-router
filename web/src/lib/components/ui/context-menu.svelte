<script>
  import { ContextMenu as ContextMenuPrimitive } from "bits-ui";

  let { children, ...restProps } = $props();
</script>

<ContextMenuPrimitive.Root {...restProps}>
  {@render children?.()}
</ContextMenuPrimitive.Root>
