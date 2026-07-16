<script>
  let { open = false, title = 'Confirm', message = '', onConfirm, onCancel } = $props();
</script>

{#if open}
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/60">
    <div class="bg-gray-900 rounded-lg p-6 w-full max-w-sm border border-gray-700">
      <h3 class="text-lg font-semibold text-gray-100 mb-2">{title}</h3>
      <p class="text-sm text-gray-400 mb-6">{message}</p>
      <div class="flex gap-3 justify-end">
        <button
          class="px-4 py-2 text-sm text-gray-400 hover:text-white"
          onclick={onCancel}
        >
          Cancel
        </button>
        <button
          class="px-4 py-2 text-sm bg-red-600 hover:bg-red-700 text-white rounded"
          onclick={onConfirm}
        >
          Delete
        </button>
      </div>
    </div>
  </div>
{/if}
