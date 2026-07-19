<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let { excludeModelName = '', onSelect = () => {}, onClose = () => {} } = $props();

  let models = $state([]);
  let loading = $state(true);
  let search = $state('');
  let cursor = $state(0);

  const filtered = $derived.by(() => {
    const q = search.toLowerCase();
    return models.filter(name => {
      if (name === excludeModelName) return false;
      if (q && !name.toLowerCase().includes(q)) return false;
      return true;
    });
  });

  function handleKeydown(e) {
    if (e.key === 'Escape') {
      onClose();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (cursor < filtered.length - 1) cursor++;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (cursor > 0) cursor--;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (filtered[cursor]) {
        onSelect(filtered[cursor]);
        onClose();
      }
    }
  }

  async function load() {
    loading = true;
    try {
      models = await apiFetch('/api/v1/metadata/models');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    search;
    cursor = 0;
  });

  onMount(() => { load(); });
</script>

<svelte:window on:keydown={handleKeydown} />

<div
  class="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
  onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}
>
  <div class="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg max-h-[70vh] flex flex-col shadow-xl">
    <div class="p-4 border-b border-gray-800">
      <input
        type="text"
        placeholder="Search models..."
        bind:value={search}
        class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-emerald-500"
      />
    </div>
    <div class="flex-1 overflow-y-auto p-2">
      {#if loading}
        <p class="text-gray-500 text-center py-4">Loading models...</p>
      {:else if filtered.length === 0}
        <p class="text-gray-500 text-center py-4">No models found</p>
      {:else}
        {#each filtered as name, i}
          <button
            class="w-full text-left px-3 py-1.5 rounded text-sm font-mono {i === cursor ? 'bg-emerald-900/50 text-emerald-300' : 'text-gray-300 hover:bg-gray-800'}"
            onclick={() => { onSelect(name); onClose(); }}
          >
            {name}
          </button>
        {/each}
      {/if}
    </div>
    <div class="p-3 border-t border-gray-800 text-xs text-gray-500 flex justify-between">
      <span>↑↓ navigate  Enter select  Esc close</span>
      <button class="hover:text-gray-300" onclick={onClose}>Cancel</button>
    </div>
  </div>
</div>
