<script>
  import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";

  let { open = $bindable(false), children, ...restProps } = $props();
</script>

<DropdownMenuPrimitive.Root bind:open {...restProps}>
  {@render children?.()}
</DropdownMenuPrimitive.Root>
