<script>
  import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<AlertDialogPrimitive.Description
  class={cn("text-sm text-muted-foreground", className)}
  {...restProps}
/>
