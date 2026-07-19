<script>
  import { Dialog as DialogPrimitive } from "bits-ui";

  let { children, ...restProps } = $props();
</script>

<DialogPrimitive.Trigger {...restProps}>
  {@render children?.()}
</DialogPrimitive.Trigger>
