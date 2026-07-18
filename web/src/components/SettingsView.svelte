<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { addToast } from '../lib/stores.js';

  let settings = $state(null);
  let loading = $state(true);
  let saving = $state(false);

  async function load() {
    loading = true;
    try {
      settings = await apiFetch('/api/v1/settings');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    try {
      settings = await apiFetch('/api/v1/settings', {
        method: 'PUT',
        body: {
          rtk_enabled: settings.rtk_enabled,
          caveman_enabled: settings.caveman_enabled,
          log_level: settings.log_level,
          max_retries: settings.max_retries,
          timeout_seconds: settings.timeout_seconds,
          max_tokens: settings.max_tokens,
        },
      });
      addToast('Settings saved', 'success');
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  onMount(load);
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-white">Settings</h2>
    <button
      class="px-3 py-1.5 text-sm bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
      onclick={load}
    >
      Refresh
    </button>
  </div>

  {#if loading}
    <div class="text-gray-400 py-8 text-center">Loading...</div>
  {:else if !settings}
    <div class="text-gray-400 py-8 text-center">Failed to load settings</div>
  {:else}
    <div class="bg-gray-900 border border-gray-800 rounded-lg p-6 space-y-6 max-w-2xl">
      <div class="flex items-center justify-between">
        <div>
          <div class="text-white font-medium">RTK Interception</div>
          <div class="text-sm text-gray-400">Intercept repetitive token requests</div>
        </div>
        <button
          class="relative inline-flex h-6 w-11 items-center rounded-full transition {settings.rtk_enabled ? 'bg-emerald-600' : 'bg-gray-700'}"
          onclick={() => settings.rtk_enabled = !settings.rtk_enabled}
        >
          <span class="inline-block h-4 w-4 transform rounded-full bg-white transition {settings.rtk_enabled ? 'translate-x-6' : 'translate-x-1'}"></span>
        </button>
      </div>

      <div class="flex items-center justify-between">
        <div>
          <div class="text-white font-medium">Caveman Mode</div>
          <div class="text-sm text-gray-400">Compress prompts to save tokens</div>
        </div>
        <button
          class="relative inline-flex h-6 w-11 items-center rounded-full transition {settings.caveman_enabled ? 'bg-emerald-600' : 'bg-gray-700'}"
          onclick={() => settings.caveman_enabled = !settings.caveman_enabled}
        >
          <span class="inline-block h-4 w-4 transform rounded-full bg-white transition {settings.caveman_enabled ? 'translate-x-6' : 'translate-x-1'}"></span>
        </button>
      </div>

      <div>
        <label class="block text-white font-medium mb-1">Log Level</label>
        <select
          class="w-full bg-gray-800 border border-gray-700 text-white rounded px-3 py-2 text-sm"
          bind:value={settings.log_level}
        >
          <option value="debug">Debug</option>
          <option value="info">Info</option>
          <option value="warn">Warn</option>
          <option value="error">Error</option>
        </select>
      </div>

      <div class="grid grid-cols-3 gap-4">
        <div>
          <label class="block text-white font-medium mb-1">Max Retries</label>
          <input
            type="number"
            min="0"
            max="10"
            class="w-full bg-gray-800 border border-gray-700 text-white rounded px-3 py-2 text-sm"
            bind:value={settings.max_retries}
          />
        </div>
        <div>
          <label class="block text-white font-medium mb-1">Timeout (s)</label>
          <input
            type="number"
            min="10"
            max="600"
            class="w-full bg-gray-800 border border-gray-700 text-white rounded px-3 py-2 text-sm"
            bind:value={settings.timeout_seconds}
          />
        </div>
        <div>
          <label class="block text-white font-medium mb-1">Max Tokens</label>
          <input
            type="number"
            min="256"
            max="1000000"
            class="w-full bg-gray-800 border border-gray-700 text-white rounded px-3 py-2 text-sm"
            bind:value={settings.max_tokens}
          />
        </div>
      </div>

      <div class="pt-2">
        <button
          class="px-4 py-2 bg-emerald-600 text-white rounded text-sm font-medium hover:bg-emerald-500 transition disabled:opacity-50"
          onclick={save}
          disabled={saving}
        >
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  {/if}
</div>
