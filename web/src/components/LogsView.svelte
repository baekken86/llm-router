<script>
  import { onMount, onDestroy } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let logs = $state([]);
  let loading = $state(true);
  let streaming = $state(false);
  let eventSource = $state(null);

  function fmtLatency(ns) {
    if (!ns) return '0ms';
    const ms = Math.round(ns / 1000000);
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function fmtTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleTimeString();
  }

  function statusColor(code) {
    if (code >= 200 && code < 300) return 'text-emerald-400';
    if (code >= 400 && code < 500) return 'text-yellow-400';
    return 'text-red-400';
  }

  async function load() {
    loading = true;
    try {
      const all = await apiFetch('/api/v1/stats/logs');
      logs = (all || []).filter(l => l.Type === 'proxy');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function toggleStream() {
    if (streaming) {
      if (eventSource) eventSource.close();
      eventSource = null;
      streaming = false;
      return;
    }

    const token = localStorage.getItem('adminToken');
    const es = new EventSource(`/api/v1/stats/logs/stream?token=${encodeURIComponent(token)}`);
    es.onmessage = (e) => {
      try {
        const log = JSON.parse(e.data);
        if (log.Type === 'proxy') {
          logs = [log, ...logs].slice(0, 500);
        }
      } catch {}
    };
    es.onerror = () => {
      streaming = false;
      eventSource = null;
    };
    eventSource = es;
    streaming = true;
  }

  onMount(load);

  onDestroy(() => {
    if (eventSource) eventSource.close();
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-white">Request Logs</h2>
    <div class="flex items-center gap-2">
      <button
        class="px-3 py-1.5 text-sm rounded transition {streaming ? 'bg-emerald-600 text-white' : 'bg-gray-800 text-gray-300 hover:bg-gray-700 hover:text-white'}"
        onclick={toggleStream}
      >
        {streaming ? 'Stop Stream' : 'Live Stream'}
      </button>
      <button
        class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
        onclick={load}
      >
        Refresh
      </button>
    </div>
  </div>

  {#if loading}
    <div class="text-gray-400 py-8 text-center">Loading...</div>
  {:else if logs.length === 0}
    <div class="text-gray-400 py-8 text-center">No logs yet</div>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-gray-800 text-gray-400">
            <th class="text-left px-4 py-2">Time</th>
            <th class="text-left px-4 py-2">VM</th>
            <th class="text-left px-4 py-2">Provider</th>
            <th class="text-left px-4 py-2">Model</th>
            <th class="text-right px-4 py-2">Status</th>
            <th class="text-right px-4 py-2">Latency</th>
            <th class="text-right px-4 py-2">In</th>
            <th class="text-right px-4 py-2">Out</th>
            <th class="text-right px-4 py-2">Cached</th>
          </tr>
        </thead>
        <tbody>
          {#each logs as log}
            <tr class="border-b border-gray-800/50 hover:bg-gray-800/30">
              <td class="px-4 py-2 text-gray-400">{fmtTime(log.Timestamp)}</td>
              <td class="px-4 py-2 text-white">{log.VirtualModel || '-'}</td>
              <td class="px-4 py-2 text-gray-300">{log.ProviderName || '-'}</td>
              <td class="px-4 py-2 text-gray-300">{log.ModelName || '-'}</td>
              <td class="px-4 py-2 text-right {statusColor(log.StatusCode)}">{log.StatusCode || '-'}</td>
              <td class="px-4 py-2 text-right text-gray-300">{fmtLatency(log.Latency)}</td>
              <td class="px-4 py-2 text-right text-gray-300">{log.InputTokens || 0}</td>
              <td class="px-4 py-2 text-right text-gray-300">{log.OutputTokens || 0}</td>
              <td class="px-4 py-2 text-right text-blue-400">{log.CachedTokens || 0}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
