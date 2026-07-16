<script>
  import { toasts } from '../lib/stores.js';
</script>

<div class="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
  {#each $toasts as toast (toast.id)}
    <div
      class="px-4 py-2 rounded shadow-lg text-sm {toast.type === 'error' ? 'bg-red-900 text-red-200' : toast.type === 'success' ? 'bg-emerald-900 text-emerald-200' : 'bg-gray-800 text-gray-200'}"
    >
      {toast.message}
    </div>
  {/each}
</div>
