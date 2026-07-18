<script>
  import { onMount } from 'svelte';
  import { adminToken, metadataFields, toasts } from './lib/stores.js';
  import { apiFetch } from './lib/api.js';
  import VirtualModelList from './components/VirtualModelList.svelte';
  import VirtualModelForm from './components/VirtualModelForm.svelte';
  import RawModelList from './components/RawModelList.svelte';
  import StatusView from './components/StatusView.svelte';
  import StatsView from './components/StatsView.svelte';
  import LogsView from './components/LogsView.svelte';
  import SyslogView from './components/SyslogView.svelte';
  import SettingsView from './components/SettingsView.svelte';
  import LoginPrompt from './components/LoginPrompt.svelte';
  import Toast from './components/Toast.svelte';

  let view = $state('list');
  let editingId = $state(null);
  let authenticated = $state(false);
  let mainTab = $state('virtual');

  function parsePath(pathname) {
    if (pathname === '/raw') return { tab: 'raw', view: 'list', id: null };
    if (pathname === '/status') return { tab: 'status', view: 'list', id: null };
    if (pathname === '/stats') return { tab: 'stats', view: 'list', id: null };
    if (pathname === '/logs') return { tab: 'logs', view: 'list', id: null };
    if (pathname === '/syslog') return { tab: 'syslog', view: 'list', id: null };
    if (pathname === '/settings') return { tab: 'settings', view: 'list', id: null };
    if (pathname === '/virtual/create') return { tab: 'virtual', view: 'create', id: null };
    if (pathname.startsWith('/virtual/')) {
      const id = pathname.split('/')[2];
      if (id) return { tab: 'virtual', view: 'edit', id };
    }
    return { tab: 'virtual', view: 'list', id: null };
  }

  function pathFor(tab, view, id) {
    if (tab === 'raw') return '/raw';
    if (tab === 'status') return '/status';
    if (tab === 'stats') return '/stats';
    if (tab === 'logs') return '/logs';
    if (tab === 'syslog') return '/syslog';
    if (tab === 'settings') return '/settings';
    if (view === 'create') return '/virtual/create';
    if (view === 'edit' && id) return `/virtual/${id}`;
    return '/virtual';
  }

  function navigate(target, id = null) {
    view = target;
    editingId = id;
    const path = pathFor(mainTab, target, id);
    history.pushState({ path }, '', path);
  }

  function navigateTab(tab) {
    mainTab = tab;
    view = 'list';
    editingId = null;
    const path = pathFor(tab, 'list', null);
    history.pushState({ path }, '', path);
  }

  function handleTabClick(e, tab) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigateTab(tab);
  }

  function handlePopState() {
    const parsed = parsePath(window.location.pathname);
    mainTab = parsed.tab;
    view = parsed.view;
    editingId = parsed.id;
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
      const parsed = parsePath(window.location.pathname);
      mainTab = parsed.tab;
      view = parsed.view;
      editingId = parsed.id;
      await loadFields();
    } catch {
      authenticated = false;
    }
  }

  function handleLogin() {
    authenticated = true;
    loadFields();
  }

  onMount(() => {
    checkAuth();
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  });
</script>

{#if !authenticated}
  <LoginPrompt onLogin={handleLogin} />
{:else}
  <div class="min-h-screen bg-gray-950">
    <nav class="bg-gray-900 border-b border-gray-800 px-6 py-3 flex flex-wrap items-center gap-x-6 gap-y-2">
      <h1 class="text-lg font-bold text-emerald-400">LLM Router</h1>
      <a
        href="/raw"
        class="text-sm no-underline {mainTab === 'raw' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'raw')}
      >
        Raw Models
      </a>
      <a
        href="/virtual"
        class="text-sm no-underline {mainTab === 'virtual' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'virtual')}
      >
        Virtual Models
      </a>
      <a
        href="/stats"
        class="text-sm no-underline {mainTab === 'stats' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'stats')}
      >
        Stats
      </a>
      <a
        href="/logs"
        class="text-sm no-underline {mainTab === 'logs' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'logs')}
      >
        Logs
      </a>
      <a
        href="/syslog"
        class="text-sm no-underline {mainTab === 'syslog' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'syslog')}
      >
        Syslog
      </a>
      <a
        href="/status"
        class="text-sm no-underline {mainTab === 'status' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'status')}
      >
        Status
      </a>
      <a
        href="/settings"
        class="text-sm no-underline {mainTab === 'settings' ? 'text-white border-b-2 border-emerald-400 pb-1' : 'text-gray-400 hover:text-white pb-1'}"
        onclick={(e) => handleTabClick(e, 'settings')}
      >
        Settings
      </a>
    </nav>

    <main class="max-w-6xl mx-auto p-6">
      {#if mainTab === 'raw'}
        <RawModelList />
      {:else if mainTab === 'status'}
        <StatusView />
      {:else if mainTab === 'stats'}
        <StatsView />
      {:else if mainTab === 'logs'}
        <LogsView />
      {:else if mainTab === 'syslog'}
        <SyslogView />
      {:else if mainTab === 'settings'}
        <SettingsView />
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
