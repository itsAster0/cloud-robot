<script lang="ts">
  import { api } from './api';
  import type { BoxProcess, BoxStats } from './types';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Button from './components/ui/button.svelte';
  import Sparkline from './Sparkline.svelte';

  // Live box metrics sampled every 3 seconds (docker stats), kept for the
  // last three minutes, plus container details and running processes.
  const KEEP = 60;
  let samples = $state<BoxStats[]>([]), processes = $state<BoxProcess[]>([]);
  let error = $state(''), paused = $state(false);
  let rates = $state<{ rx: number; tx: number }[]>([]);

  async function sample() {
    try {
      const next = await api.boxStats();
      const prev = samples[samples.length - 1];
      if (prev) {
        const seconds = Math.max(0.5, (Date.parse(next.at) - Date.parse(prev.at)) / 1000);
        rates = [...rates, { rx: Math.max(0, (next.netRxBytes - prev.netRxBytes) / seconds), tx: Math.max(0, (next.netTxBytes - prev.netTxBytes) / seconds) }].slice(-KEEP);
      }
      samples = [...samples, next].slice(-KEEP);
      error = '';
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function loadProcesses() {
    try { processes = (await api.boxProcesses()).processes; } catch { /* the table keeps its last rows */ }
  }
  $effect(() => {
    if (paused) return;
    let alive = true, timer = 0;
    const tick = async () => { await sample(); if (samples.length % 3 === 1) await loadProcesses(); if (alive) timer = window.setTimeout(tick, 3000); };
    void tick();
    return () => { alive = false; clearTimeout(timer); };
  });

  const latest = $derived(samples[samples.length - 1]);
  const bytes = (n = 0) => n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KB` : `${Math.round(n)} B`;
  const uptime = (iso?: string) => {
    if (!iso) return '—';
    const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
    return s > 86400 ? `${Math.floor(s / 86400)}d ${Math.floor(s % 86400 / 3600)}h` : s > 3600 ? `${Math.floor(s / 3600)}h ${Math.floor(s % 3600 / 60)}m` : `${Math.floor(s / 60)}m ${Math.floor(s % 60)}s`;
  };
  const series = $derived([
    { label: 'CPU', value: latest ? `${latest.cpuPercent.toFixed(1)}%` : '—', values: samples.map(s => s.cpuPercent), max: 100, color: 'var(--color-primary)' },
    { label: 'Memory', value: latest ? `${bytes(latest.memoryBytes)} of ${bytes(latest.memoryLimitBytes)}` : '—', values: samples.map(s => s.memoryPercent), max: 100, color: '#6aa8ff' },
    { label: 'Processes', value: latest ? String(latest.pids) : '—', values: samples.map(s => s.pids), max: undefined, color: '#c08cff' },
    { label: 'Network in / out', value: rates.length ? `${bytes(rates[rates.length - 1].rx)}/s · ${bytes(rates[rates.length - 1].tx)}/s` : '—', values: rates.map(r => r.rx + r.tx), max: undefined, color: '#f0c274' },
  ]);
</script>

<Card>
  <CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
    <div><CardTitle>Live monitor</CardTitle><CardDescription>Sampled every 3 seconds from the container. Graphs show the last 3 minutes.</CardDescription></div>
    <Button variant="outline" size="sm" onclick={() => paused = !paused}>{paused ? 'Resume' : 'Pause'}</Button>
  </CardHeader>
  <CardContent class="grid gap-4">
    {#if error}<p class="m-0 text-sm text-destructive" role="alert">{error}</p>{/if}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {#each series as s}
        <div class="grid gap-1 rounded-lg border border-border bg-background p-3">
          <div class="flex items-baseline justify-between gap-2"><span class="text-xs text-muted-foreground">{s.label}</span><strong class="truncate font-mono text-sm">{s.value}</strong></div>
          <Sparkline values={s.values} max={s.max} color={s.color} label={`${s.label} over time`}/>
        </div>
      {/each}
    </div>
    <dl class="m-0 grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
      {#each [['Container', latest?.containerId ?? '—'], ['State', latest?.state ?? '—'], ['Uptime', uptime(latest?.startedAt)], ['Restarts', String(latest?.restarts ?? '—')], ['Image', latest?.image ?? '—'], ['Created', latest?.createdAt ? new Date(latest.createdAt).toLocaleString() : '—'], ['Disk read', bytes(latest?.blockReadBytes)], ['Disk written', bytes(latest?.blockWriteBytes)]] as [label, value]}
        <div class="min-w-0"><dt class="text-xs text-muted-foreground">{label}</dt><dd class="m-0 truncate font-mono" title={value}>{value}</dd></div>
      {/each}
    </dl>
    <div class="grid gap-2">
      <div class="flex items-center justify-between"><strong class="text-sm">Processes</strong><Button variant="ghost" size="sm" onclick={loadProcesses}>Refresh</Button></div>
      <div class="max-h-56 overflow-auto rounded-md border border-border">
        <table class="w-full border-collapse font-mono text-xs">
          <thead class="sticky top-0 bg-card text-left text-muted-foreground"><tr><th class="px-2 py-1.5">PID</th><th class="px-2">User</th><th class="px-2 text-right">Memory</th><th class="px-2">Running</th><th class="px-2">CPU time</th><th class="px-2">Command</th></tr></thead>
          <tbody>
            {#each processes as p (p.pid)}
              <tr class="border-t border-border/60 {/lua/.test(p.command) ? 'bg-accent' : ''}"><td class="px-2 py-1">{p.pid}</td><td class="px-2">{p.user}</td><td class="px-2 text-right">{bytes(Number(p.rssKb) * 1024)}</td><td class="px-2">{p.elapsed}</td><td class="px-2">{p.cpuTime}</td><td class="max-w-[28rem] truncate px-2" title={p.command}>{p.command}</td></tr>
            {:else}<tr><td colspan="6" class="px-2 py-3 text-muted-foreground">Loading processes…</td></tr>{/each}
          </tbody>
        </table>
      </div>
      <p class="m-0 text-xs text-muted-foreground">Highlighted rows run Lua: that is your robot's program.</p>
    </div>
  </CardContent>
</Card>
