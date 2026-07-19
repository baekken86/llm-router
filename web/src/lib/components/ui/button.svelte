<script>
  import { cn } from "$lib/utils.js";
  import { buttonVariants } from "./button-variants.js";

  let {
    class: className,
    variant = "default",
    size = "default",
    href = undefined,
    type = "button",
    disabled,
    children,
    ...restProps
  } = $props();
</script>

{#if href}
  <a
    class={cn(buttonVariants({ variant, size }), className)}
    href={disabled ? undefined : href}
    aria-disabled={disabled}
    role={disabled ? "link" : undefined}
    tabindex={disabled ? -1 : undefined}
    {...restProps}
  >
    {@render children?.()}
  </a>
{:else}
  <button
    class={cn(buttonVariants({ variant, size }), className)}
    {type}
    {disabled}
    {...restProps}
  >
    {@render children?.()}
  </button>
{/if}
