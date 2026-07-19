<script>
  import { Select as SelectPrimitive } from "bits-ui";

  let { value = $bindable(""), type, children, ...restProps } = $props();
</script>

<SelectPrimitive.Root bind:value {type} {...restProps}>
  {@render children?.()}
</SelectPrimitive.Root>
