<script>
  import { DragDropProvider, DragOverlay } from '@dnd-kit/svelte';

  let { children, onDragEnd } = $props();
</script>

<DragDropProvider {onDragEnd}>
  {@render children?.()}

  <DragOverlay>
    <div class="bg-gray-800 border border-emerald-500 rounded px-2 py-1 text-sm text-gray-200 shadow-lg opacity-80 pointer-events-none whitespace-nowrap">
      Dragging...
    </div>
  </DragOverlay>
</DragDropProvider>
