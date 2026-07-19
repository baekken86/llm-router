<script>
  import { cn } from "$lib/utils.js";

  let { class: className, value, active = false, children, onclick, ...restProps } = $props();
</script>

<button
  role="tab"
  aria-selected={active}
  data-state={active ? "active" : "inactive"}
  class={cn(
    "inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium ring-offset-background transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
    active && "bg-background text-foreground shadow",
    className
  )}
  {onclick}
  {...restProps}
>
  {@render children?.()}
</button>
