<script>
  import { onMount } from 'svelte';
  import { adminToken, metadataFields, toasts } from './lib/stores.js';
  import { apiFetch } from './lib/api.js';
  import VirtualModelList from './components/VirtualModelList.svelte';
  import VirtualModelForm from './components/VirtualModelForm.svelte';
  import RawModelList from './components/RawModelList.svelte';
  import LoginPrompt from './components/LoginPrompt.svelte';
  import Toast from './components/Toast.svelte';

  let view = $state('list');
  let editingId = $state(null);
  let authenticated = $state(false);
  let mainTab = $state('virtual');

  function navigate(target, id = null) {
    view = target;
    editingId = id;
  }

  async function loadFields() {
    try {
      const data = await apiFetch('/api/v1/metadata/fields');
      metadataFields.set(data.fields);
    } catch (e) {
      console.error('Failed to load metadata fields:', e);
    }
  }

  async function checkAuth() {
    const token = localStorage.getItem('adminToken');
    if (!token) return;
    adminToken.set(token);
    try {
      await apiFetch('/api/v1/virtual-models');
      authenticated = true;
      await loadFields();
    } catch {
      authenticated = false;
    }
  }

  function handleLogin() {
    authenticated = true;
    loadFields();
  }

  onMount(checkAuth);
</script>

{#if !authenticated}
  <LoginPrompt onLogin={handleLogin} />
{:else}
  <div class="min-h-screen bg-gray-950">
    <nav class="bg-gray-900 border-b border-gray-800 px-6 py-3 flex items-center gap-6">
      <h1 class="text-lg font-bold text-emerald-400">LLM Router</h1>
      <button
        class="text-sm {mainTab === 'raw' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={() => { mainTab = 'raw'; view = 'list'; }}
      >
        Raw Models
      </button>
      <button
        class="text-sm {mainTab === 'virtual' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={() => { mainTab = 'virtual'; view = 'list'; }}
      >
        Virtual Models
      </button>
    </nav>

    <main class="max-w-6xl mx-auto p-6">
      {#if mainTab === 'raw'}
        <RawModelList />
      {:else if view === 'list'}
        <VirtualModelList
          onCreate={() => navigate('create')}
          onEdit={(id) => navigate('edit', id)}
        />
      {:else}
        <VirtualModelForm
          vmId={editingId}
          onBack={() => navigate('list')}
        />
      {/if}
    </main>
  </div>
{/if}

<Toast />
