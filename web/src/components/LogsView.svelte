<script>
  import { onMount, onDestroy } from 'svelte';
  import { apiFetch } from '$lib/api.js';
  import { addToast } from '$lib/stores.js';
  import Button from '$lib/components/ui/button.svelte';
  import Card from '$lib/components/ui/card.svelte';
  import Badge from '$lib/components/ui/badge.svelte';
  import Table from '$lib/components/ui/table.svelte';
  import TableElement from '$lib/components/ui/table-element.svelte';
  import TableHeader from '$lib/components/ui/table-header.svelte';
  import TableBody from '$lib/components/ui/table-body.svelte';
  import TableRow from '$lib/components/ui/table-row.svelte';
  import TableHead from '$lib/components/ui/table-head.svelte';
  import TableCell from '$lib/components/ui/table-cell.svelte';
  import { RefreshCw, Radio } from '@lucide/svelte';

  let logs = $state([]);
  let loading = $state(true);
  let streaming = $state(false);
  let eventSource = $state(null);
  let reconnectTimer = null;

  function fmtLatency(ns) {
    if (!ns) return '0ms';
    const ms = Math.round(ns / 1000000);
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function fmtTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleTimeString('en-GB', { hour12: false });
  }

  function statusColor(code) {
    if (code >= 200 && code < 300) return 'text-emerald-400';
    if (code >= 400 && code < 500) return 'text-yellow-400';
    return 'text-destructive';
  }

  async function load() {
    loading = true;
    try {
      const all = await apiFetch('/api/v1/stats/logs');
      logs = all || [];
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function startStream() {
    const token = localStorage.getItem('adminToken');
    const es = new EventSource(`/api/v1/stats/logs/stream?token=${encodeURIComponent(token)}`);
    es.onmessage = (e) => {
      try {
        const log = JSON.parse(e.data);
        logs = [log, ...logs].slice(0, 500);
      } catch {}
    };
    es.onerror = () => {
      es.close();
      eventSource = null;
      if (streaming) {
        reconnectTimer = setTimeout(() => { startStream(); }, 3000);
      }
    };
    eventSource = es;
  }

  function toggleStream() {
    if (streaming) {
      clearTimeout(reconnectTimer);
      if (eventSource) eventSource.close();
      eventSource = null;
      streaming = false;
      return;
    }

    streaming = true;
    startStream();
  }

  onMount(load);

  onDestroy(() => {
    clearTimeout(reconnectTimer);
    if (eventSource) eventSource.close();
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-foreground">Request Logs</h2>
    <div class="flex items-center gap-2">
      <Button
        variant={streaming ? "default" : "outline"}
        size="sm"
        onclick={toggleStream}
        class={streaming ? "bg-emerald-600 hover:bg-emerald-700" : ""}
      >
        <Radio class="h-4 w-4 mr-1" />
        {streaming ? 'Stop Stream' : 'Live Stream'}
      </Button>
      <Button variant="outline" size="sm" onclick={load}>
        <RefreshCw class="h-4 w-4 mr-1" />
        Refresh
      </Button>
    </div>
  </div>

  {#if loading}
    <div class="text-muted-foreground py-8 text-center">Loading...</div>
  {:else if logs.length === 0}
    <div class="text-muted-foreground py-8 text-center">No logs yet</div>
  {:else}
    <Card class="overflow-hidden">
      <Table>
        <TableElement>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>VM</TableHead>
              <TableHead>Provider</TableHead>
              <TableHead>Model</TableHead>
              <TableHead class="text-right">Status</TableHead>
              <TableHead class="text-right">Latency</TableHead>
              <TableHead class="text-right">In</TableHead>
              <TableHead class="text-right">Out</TableHead>
              <TableHead class="text-right">Cached</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each logs as log}
              <TableRow>
                <TableCell class="text-muted-foreground font-mono text-xs">{fmtTime(log.Timestamp)}</TableCell>
                <TableCell class="text-foreground">{log.VirtualModel || '-'}</TableCell>
                <TableCell>{log.ProviderName || '-'}</TableCell>
                <TableCell>{log.ModelName || '-'}</TableCell>
                <TableCell class="text-right {statusColor(log.StatusCode)}">{log.StatusCode || '-'}</TableCell>
                <TableCell class="text-right">{fmtLatency(log.Latency)}</TableCell>
                <TableCell class="text-right">{log.InputTokens || 0}</TableCell>
                <TableCell class="text-right">{log.OutputTokens || 0}</TableCell>
                <TableCell class="text-right text-blue-400">{log.CachedTokens || 0}</TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </TableElement>
      </Table>
    </Card>
  {/if}
</div>
