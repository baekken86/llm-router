<script>
  let { onLogin } = $props();
  let password = $state('');
  let error = $state('');

  async function submit() {
    error = '';
    if (!password.trim()) {
      error = 'Password required';
      return;
    }
    try {
      const res = await fetch('/api/v1/admin/auth', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });
      if (!res.ok) throw new Error('Invalid password');
      const data = await res.json();
      localStorage.setItem('adminToken', data.token);
      const { adminToken } = await import('../lib/stores.js');
      adminToken.set(data.token);
      onLogin();
    } catch {
      error = 'Invalid password';
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-gray-950">
  <div class="bg-gray-900 rounded-lg p-8 w-full max-w-md border border-gray-800">
    <h1 class="text-xl font-bold text-emerald-400 mb-6">LLM Router</h1>
    <form onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <label class="block text-sm text-gray-400 mb-2">Admin Password</label>
      <input
        type="password"
        bind:value={password}
        placeholder="Enter password"
        class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-emerald-500 mb-3"
      />
      {#if error}
        <p class="text-red-400 text-sm mb-3">{error}</p>
      {/if}
      <button
        type="submit"
        class="w-full bg-emerald-600 hover:bg-emerald-700 text-white rounded px-3 py-2 text-sm font-medium"
      >
        Connect
      </button>
    </form>
  </div>
</div>
