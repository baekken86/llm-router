<script>
  import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<AlertDialogPrimitive.Title
  class={cn("text-lg font-semibold", className)}
  {...restProps}
/>
