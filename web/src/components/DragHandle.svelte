<script>
  /**
   * Grip icon component for drag handle.
   * Attach via {@attach sortable.attachHandle} from createSortable.
   */
  let { attachHandle, ariaLabel = 'Drag to reorder' } = $props();
</script>

<button
  {@attach attachHandle}
  class="cursor-grab active:cursor-grabbing text-gray-500 hover:text-gray-300 select-none p-0.5"
  aria-label={ariaLabel}
  title={ariaLabel}
  tabindex="-1"
>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="14"
    height="14"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <circle cx="9" cy="6" r="1" />
    <circle cx="15" cy="6" r="1" />
    <circle cx="9" cy="12" r="1" />
    <circle cx="15" cy="12" r="1" />
    <circle cx="9" cy="18" r="1" />
    <circle cx="15" cy="18" r="1" />
  </svg>
</button>
