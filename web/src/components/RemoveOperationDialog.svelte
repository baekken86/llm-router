<script>
  import { tick } from 'svelte';

  let { open = false, title = 'Remove operation', message = '', onRemove, onFlatten, onCancel } = $props();
  const id = $props.id();
  let panel = $state();
  let cancelButton = $state();

  $effect(() => {
    if (!open) return;
    const previous = document.activeElement;
    let active = true;
    tick().then(() => { if (active) cancelButton?.focus(); });
    return () => {
      active = false;
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  });

  function handleKey(event) {
    if (!open) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onCancel?.();
    } else if (event.key === 'Tab') {
      const buttons = Array.from(panel?.querySelectorAll('button') ?? []);
      const index = buttons.indexOf(document.activeElement);
      event.preventDefault();
      buttons[(index + (event.shiftKey ? buttons.length - 1 : 1)) % buttons.length]?.focus();
    }
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if open}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
    <button type="button" tabindex="-1" aria-label="Cancel removal" class="absolute inset-0 w-full h-full" onclick={onCancel}></button>
    <div bind:this={panel} role="dialog" aria-modal="true" aria-labelledby={`${id}-title`} aria-describedby={`${id}-description`} tabindex="-1" class="relative bg-gray-900 rounded-lg p-6 w-full max-w-sm border border-gray-700">
      <h3 id={`${id}-title`} class="text-lg font-semibold text-gray-100 mb-2">{title}</h3>
      <p id={`${id}-description`} class="text-sm text-gray-400 mb-6">{message}</p>
      <div class="flex flex-wrap gap-3 justify-end">
        <button type="button" class="px-3 py-2 text-sm bg-red-600 hover:bg-red-700 text-white rounded" onclick={onRemove}>Remove with children</button>
        <button type="button" class="px-3 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded" onclick={onFlatten}>Flatten children</button>
        <button bind:this={cancelButton} type="button" class="px-3 py-2 text-sm text-gray-400 hover:text-white" onclick={onCancel}>Cancel</button>
      </div>
    </div>
  </div>
{/if}
