<script>
  import { AlertDialog as AlertDialogPrimitive } from "bits-ui";

  let { children, ...restProps } = $props();
</script>

<AlertDialogPrimitive.Trigger {...restProps}>
  {@render children?.()}
</AlertDialogPrimitive.Trigger>
