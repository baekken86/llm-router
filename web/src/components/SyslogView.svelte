<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let entries = $state([]);
  let loading = $state(true);

  function fmtTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleString();
  }

  function levelColor(level) {
    switch (level) {
      case 'ERROR': return 'text-red-400';
      case 'WARN': return 'text-yellow-400';
      case 'DEBUG': return 'text-gray-400';
      default: return 'text-emerald-400';
    }
  }

  async function load() {
    loading = true;
    try {
      entries = await apiFetch('/api/v1/syslog');
      if (!entries) entries = [];
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-white">System Log</h2>
    <button
      class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
      onclick={load}
    >
      Refresh
    </button>
  </div>

  {#if loading}
    <div class="text-gray-400 py-8 text-center">Loading...</div>
  {:else if entries.length === 0}
    <div class="text-gray-400 py-8 text-center">No syslog entries</div>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-gray-800 text-gray-400">
            <th class="text-left px-4 py-2 w-48">Timestamp</th>
            <th class="text-left px-4 py-2 w-20">Level</th>
            <th class="text-left px-4 py-2">Message</th>
          </tr>
        </thead>
        <tbody>
          {#each entries as entry}
            <tr class="border-b border-gray-800/50 hover:bg-gray-800/30">
              <td class="px-4 py-2 text-gray-400 font-mono text-xs">{fmtTime(entry.timestamp)}</td>
              <td class="px-4 py-2 {levelColor(entry.level)} font-medium">{entry.level}</td>
              <td class="px-4 py-2 text-gray-300 font-mono text-xs break-all">{entry.message}</td>
            </tr>
          {/each}
        </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>
