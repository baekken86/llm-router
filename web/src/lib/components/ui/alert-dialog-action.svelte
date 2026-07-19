<script>
  import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
  import { buttonVariants } from "./button-variants.js";
  import { cn } from "$lib/utils.js";

  let { class: className, ...restProps } = $props();
</script>

<AlertDialogPrimitive.Action
  class={cn(buttonVariants(), className)}
  {...restProps}
/>
