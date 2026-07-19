<script>
  import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
  import { buttonVariants } from "./button-variants.js";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<AlertDialogPrimitive.Cancel
  class={cn(buttonVariants({ variant: "outline" }), "mt-2 sm:mt-0", className)}
  {...restProps}
/>
