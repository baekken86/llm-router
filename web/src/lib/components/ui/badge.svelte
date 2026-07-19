<script>
  import { cn } from "$lib/utils.js";
  import { tv } from "tailwind-variants";

  const badgeVariants = tv({
    base: "inline-flex items-center rounded-md border border-transparent px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow hover:bg-primary/80",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive: "bg-destructive text-destructive-foreground shadow hover:bg-destructive/80",
        outline: "border-border text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  });

  let { class: className, variant = "default", children, ...restProps } = $props();
</script>

<div class={cn(badgeVariants({ variant }), className)} {...restProps}>
  {@render children?.()}
</div>
