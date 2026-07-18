<script>
  import { onMount, onDestroy } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let stats = $state(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    try {
      stats = await apiFetch('/api/v1/stats');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function fmt(n) {
    if (n == null) return '0';
    return n.toLocaleString();
  }

  function pct(a, b) {
    if (!b) return '0';
    return ((a / b) * 100).toFixed(1);
  }

  onMount(load);
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-white">Statistics</h2>
    <button
      class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
      onclick={load}
    >
      Refresh
    </button>
  </div>

  {#if loading}
    <div class="text-gray-400 py-8 text-center">Loading...</div>
  {:else if !stats}
    <div class="text-gray-400 py-8 text-center">No stats available</div>
  {:else}
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Total Requests</div>
        <div class="text-2xl font-bold text-white mt-1">{fmt(stats.total_requests)}</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Successes</div>
        <div class="text-2xl font-bold text-emerald-400 mt-1">{fmt(stats.successes)}</div>
        <div class="text-xs text-gray-500">{pct(stats.successes, stats.total_requests)}%</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Failures</div>
        <div class="text-2xl font-bold text-red-400 mt-1">{fmt(stats.failures)}</div>
        <div class="text-xs text-gray-500">{pct(stats.failures, stats.total_requests)}%</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Input Tokens</div>
        <div class="text-2xl font-bold text-white mt-1">{fmt(stats.input_tokens)}</div>
      </div>
    </div>

    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Output Tokens</div>
        <div class="text-2xl font-bold text-white mt-1">{fmt(stats.output_tokens)}</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Cached Tokens</div>
        <div class="text-2xl font-bold text-blue-400 mt-1">{fmt(stats.cached_tokens)}</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">Reasoning Tokens</div>
        <div class="text-2xl font-bold text-purple-400 mt-1">{fmt(stats.reasoning_tokens)}</div>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <div class="text-sm text-gray-400">RTK Intercepts</div>
        <div class="text-2xl font-bold text-yellow-400 mt-1">{fmt(stats.rtk_intercepts)}</div>
        <div class="text-xs text-gray-500">{fmt(stats.rtk_saved_tokens)} tokens saved</div>
      </div>
    </div>

    {#if stats.by_virtual_model && Object.keys(stats.by_virtual_model).length > 0}
      <div>
        <h3 class="text-lg font-medium text-white mb-3">By Virtual Model</h3>
        <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-800 text-gray-400">
                <th class="text-left px-4 py-2">Model</th>
                <th class="text-right px-4 py-2">Requests</th>
                <th class="text-right px-4 py-2">Success</th>
                <th class="text-right px-4 py-2">Fail</th>
                <th class="text-right px-4 py-2">Input</th>
                <th class="text-right px-4 py-2">Output</th>
                <th class="text-right px-4 py-2">Cached</th>
              </tr>
            </thead>
            <tbody>
              {#each Object.entries(stats.by_virtual_model) as [name, s]}
                <tr class="border-b border-gray-800/50 hover:bg-gray-800/30">
                  <td class="px-4 py-2 text-white">{name}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.requests)}</td>
                  <td class="px-4 py-2 text-right text-emerald-400">{fmt(s.successes)}</td>
                  <td class="px-4 py-2 text-right text-red-400">{fmt(s.failures)}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.input_tokens)}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.output_tokens)}</td>
                  <td class="px-4 py-2 text-right text-blue-400">{fmt(s.cached_tokens)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/if}

    {#if stats.by_provider && Object.keys(stats.by_provider).length > 0}
      <div>
        <h3 class="text-lg font-medium text-white mb-3">By Provider / Model</h3>
        <div class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-800 text-gray-400">
                <th class="text-left px-4 py-2">Provider / Model</th>
                <th class="text-right px-4 py-2">Requests</th>
                <th class="text-right px-4 py-2">Success</th>
                <th class="text-right px-4 py-2">Fail</th>
                <th class="text-right px-4 py-2">Input</th>
                <th class="text-right px-4 py-2">Output</th>
                <th class="text-right px-4 py-2">Cached</th>
              </tr>
            </thead>
            <tbody>
              {#each Object.entries(stats.by_provider) as [name, s]}
                <tr class="border-b border-gray-800/50 hover:bg-gray-800/30">
                  <td class="px-4 py-2 text-white">{name}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.requests)}</td>
                  <td class="px-4 py-2 text-right text-emerald-400">{fmt(s.successes)}</td>
                  <td class="px-4 py-2 text-right text-red-400">{fmt(s.failures)}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.input_tokens)}</td>
                  <td class="px-4 py-2 text-right text-gray-300">{fmt(s.output_tokens)}</td>
                  <td class="px-4 py-2 text-right text-blue-400">{fmt(s.cached_tokens)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/if}
  {/if}
</div>
