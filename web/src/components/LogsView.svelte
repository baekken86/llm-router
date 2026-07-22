<script>
  import { onMount, onDestroy } from 'svelte';
  import { apiFetch } from '$lib/api.js';
  import { addToast } from '$lib/stores.js';
  import Button from '$lib/components/ui/button.svelte';
  import Card from '$lib/components/ui/card.svelte';
  import Badge from '$lib/components/ui/badge.svelte';
  import { RefreshCw, ChevronRight, ChevronDown } from '@lucide/svelte';

  let logs = $state([]);
  let loading = $state(true);
  let eventSource = $state(null);
  let reconnectTimer = null;
  let expanded = $state(new Set());

  function fmtLatency(ns) {
    if (!ns) return '0ms';
    const ms = Math.round(ns / 1000000);
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function fmtDuration(ms) {
    if (!ms) return '0s';
    const s = Math.round(ms / 1000);
    if (s < 60) return `${s}s`;
    return `${Math.floor(s / 60)}m${s % 60}s`;
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

  function fmtNum(n) {
    if (!n) return '0';
    if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`;
    if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
    return String(n);
  }

  function elapsed(ts) {
    if (!ts) return '';
    const ms = Date.now() - new Date(ts).getTime();
    return fmtDuration(ms);
  }

  let grouped = $derived.by(() => {
    const childMap = {};
    const parentOrder = [];
    for (const log of logs) {
      if (log.Type === 'incoming') {
        if (childMap[log.RequestID]) {
          childMap[log.RequestID].VirtualModel = log.VirtualModel;
          childMap[log.RequestID].Timestamp = log.Timestamp;
        } else {
          const entry = { ...log, children: [] };
          childMap[log.RequestID] = entry;
          parentOrder.push(log.RequestID);
        }
      } else {
        const key = log.RequestID;
        if (!childMap[key]) {
          childMap[key] = {
            Type: 'incoming',
            RequestID: log.RequestID,
            VirtualModel: log.VirtualModel,
            Timestamp: log.Timestamp,
            children: [],
          };
          parentOrder.push(key);
        }
        const existing = childMap[key].children.findIndex(
          c => c.ProviderName === log.ProviderName && c.ModelName === log.ModelName
        );
        if (existing >= 0) {
          childMap[key].children[existing] = log;
        } else {
          childMap[key].children.push(log);
        }
      }
    }
    return parentOrder.map(id => childMap[id]);
  });

  async function load() {
    loading = true;
    try {
      const all = await apiFetch('/api/v1/stats/logs');
      logs = all || [];
      expanded = new Set(parentsWhereShouldExpand(logs));
    } catch (e) {
      addToast(e.message, 'error');
    } finally {
      loading = false;
    }
  }

  function parentsWhereShouldExpand(all) {
    const ids = new Set();
    for (const log of all) {
      if (log.Type === 'incoming') {
        ids.add(log.RequestID);
      }
    }
    return ids;
  }

  function toggleExpand(requestID) {
    const next = new Set(expanded);
    if (next.has(requestID)) {
      next.delete(requestID);
    } else {
      next.add(requestID);
    }
    expanded = next;
  }

  function startStream() {
    const token = localStorage.getItem('adminToken');
    const es = new EventSource(`/api/v1/stats/logs/stream?token=${encodeURIComponent(token)}`);
    es.onmessage = (e) => {
      try {
        const log = JSON.parse(e.data);
        logs = upsertLog(logs, log);
        if (log.Type === 'incoming' && !expanded.has(log.RequestID)) {
          expanded = new Set([...expanded, log.RequestID]);
        }
      } catch {}
    };
    es.onerror = () => {
      es.close();
      eventSource = null;
      reconnectTimer = setTimeout(() => { startStream(); }, 3000);
    };
    eventSource = es;
  }

  function upsertLog(current, log) {
    const next = [...current];
    if (log.Type === 'incoming') {
      const idx = next.findIndex(l => l.Type === 'incoming' && l.RequestID === log.RequestID);
      if (idx >= 0) {
        next[idx] = log;
      } else {
        next.unshift(log);
      }
      return next.slice(0, 500);
    }

    const idx = next.findIndex(
      l => l.Type === 'proxy' && l.RequestID === log.RequestID &&
           l.ProviderName === log.ProviderName && l.ModelName === log.ModelName
    );
    if (idx >= 0) {
      next[idx] = log;
    } else {
      next.unshift(log);
    }

    if (log.Status === 'streaming') {
      const parentIdx = next.findIndex(l => l.Type === 'incoming' && l.RequestID === log.RequestID);
      if (parentIdx < 0) {
        next.unshift({
          Type: 'incoming',
          RequestID: log.RequestID,
          VirtualModel: log.VirtualModel,
          Timestamp: log.Timestamp,
        });
      }
    }

    return next.slice(0, 500);
  }

  onMount(() => {
    load();
    startStream();
  });

  onDestroy(() => {
    clearTimeout(reconnectTimer);
    if (eventSource) eventSource.close();
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-xl font-semibold text-foreground">Request Logs</h2>
    <Button variant="outline" size="sm" onclick={load}>
      <RefreshCw class="h-4 w-4 mr-1" />
      Refresh
    </Button>
  </div>

  {#if loading}
    <div class="text-muted-foreground py-8 text-center">Loading...</div>
  {:else if grouped.length === 0}
    <div class="text-muted-foreground py-8 text-center">No logs yet</div>
  {:else}
    <Card class="overflow-hidden">
      <div class="overflow-x-auto divide-y divide-border">
        {#each grouped as parent (parent.RequestID)}
          <div>
            <button
              class="w-full flex items-center gap-2 px-4 py-2 text-left hover:bg-muted/50 transition-colors"
              onclick={() => toggleExpand(parent.RequestID)}
            >
              {#if parent.children && parent.children.length > 0}
                {#if expanded.has(parent.RequestID)}
                  <ChevronDown class="h-4 w-4 text-muted-foreground shrink-0" />
                {:else}
                  <ChevronRight class="h-4 w-4 text-muted-foreground shrink-0" />
                {/if}
              {:else}
                <span class="w-4 shrink-0"></span>
              {/if}
              <span class="text-xs text-muted-foreground font-mono w-16 shrink-0">{fmtTime(parent.Timestamp)}</span>
              <span class="text-sm font-medium text-foreground">{parent.VirtualModel || '-'}</span>
              {#if parent.children?.some(c => c.Status === 'streaming')}
                <span class="inline-block w-2 h-2 bg-blue-400 rounded-full animate-pulse ml-1" title="in progress"></span>
              {:else if parent.children && parent.children.length > 0}
                {#if parent.children.some(c => c.StatusCode >= 400 || c.Status === 'failed')}
                  <span class="text-destructive ml-1 text-xs">failed</span>
                {:else}
                  <span class="text-emerald-400 ml-1 text-xs">done</span>
                {/if}
              {/if}
            </button>

            {#if expanded.has(parent.RequestID) && parent.children}
              <div class="ml-6 border-l-2 border-border">
                {#each parent.children as child}
                  {@const isStreaming = child.Status === 'streaming'}
                  <div class="flex items-center gap-2 px-4 py-1.5 text-xs {isStreaming ? 'opacity-70' : ''}">
                    <span class="text-muted-foreground font-mono w-16 shrink-0">{fmtTime(child.Timestamp)}</span>
                    <span class="text-muted-foreground w-24 shrink-0">{child.ProviderName || '-'}</span>
                    <span class="text-foreground w-40 shrink-0 truncate">{child.ModelName || '-'}</span>
                    <span class="w-16 text-right shrink-0 {isStreaming ? 'text-blue-400' : statusColor(child.StatusCode)}">
                      {#if isStreaming}
                        <span class="inline-block w-2 h-2 bg-blue-400 rounded-full animate-pulse mr-1"></span>streaming
                      {:else}
                        {child.StatusCode || '-'}
                      {/if}
                    </span>
                    <span class="text-muted-foreground w-20 text-right shrink-0">
                      {isStreaming ? elapsed(child.Timestamp) : fmtLatency(child.Latency)}
                    </span>
                    <span class="text-foreground w-16 text-right shrink-0">{fmtNum(child.InputTokens)}</span>
                    <span class="text-foreground w-16 text-right shrink-0">{fmtNum(child.OutputTokens)}</span>
                    <span class="text-blue-400 w-16 text-right shrink-0">{fmtNum(child.CachedTokens)}</span>
                    {#if child.ErrorMessage}
                      <span class="text-destructive ml-1 truncate max-w-48">{child.ErrorMessage}</span>
                    {/if}
                    {#if child.Fallback > 0}
                      <span class="text-yellow-400">fb={child.Fallback}</span>
                    {/if}
                    {#if child.Retry > 0}
                      <span class="text-yellow-400">retry={child.Retry}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </Card>
  {/if}
</div>
