<script lang="ts">
  import { api } from './api';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Button from './components/ui/button.svelte';
  import Input from './components/ui/input.svelte';
  import Select from './components/ui/select.svelte';

  // Box output viewer: filter, errors-only, follow (auto refresh and
  // scroll), tail size, clear (hides everything before now, remembered per
  // browser), copy, and download. The container log itself is never erased.
  const CLEAR_KEY = 'robot-arena:box-logs-since';
  let raw = $state(''), loading = $state(false), error = $state('');
  let filter = $state(''), errorsOnly = $state(false), follow = $state(true), tail = $state('500'), wrap = $state(true);
  let since = $state((() => { try { return localStorage.getItem(CLEAR_KEY) ?? ''; } catch { return ''; } })());
  let copied = $state(false);
  let viewer: HTMLPreElement | undefined = $state();

  async function load() {
    loading = true;
    try { raw = (await api.getBoxLogs(Number(tail), since)).logs ?? ''; error = ''; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loading = false; }
    if (follow) queueMicrotask(() => viewer?.scrollTo({ top: viewer.scrollHeight }));
  }
  $effect(() => { void tail; void since; void load(); });
  $effect(() => {
    if (!follow) return;
    const timer = setInterval(() => void load(), 4000);
    return () => clearInterval(timer);
  });

  const isError = (line: string) => /error|failed|rejected|exited|traceback|panic/i.test(line);
  const lines = $derived(raw.split('\n').filter(l => l.trim()));
  const shown = $derived(lines.filter(l => (!errorsOnly || isError(l)) && (!filter || l.toLowerCase().includes(filter.toLowerCase()))));
  const errorCount = $derived(lines.filter(isError).length);

  function clearView() {
    since = new Date().toISOString();
    try { localStorage.setItem(CLEAR_KEY, since); } catch { /* clear lasts for this page only */ }
  }
  function showAll() {
    since = '';
    try { localStorage.removeItem(CLEAR_KEY); } catch { /* optional */ }
  }
  async function copy() {
    try { await navigator.clipboard.writeText(shown.join('\n')); copied = true; setTimeout(() => copied = false, 1500); } catch { error = 'Copy is not allowed in this browser.'; }
  }
  function download() {
    const url = URL.createObjectURL(new Blob([shown.join('\n') + '\n'], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url; a.download = `box-output-${new Date().toISOString().replace(/[:.]/g, '-')}.log`; a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  // Split docker's RFC 3339 timestamp from the message for display.
  const split = (line: string) => { const m = line.match(/^(\d{4}-\d\d-\d\dT[\d:.]+Z)\s(.*)$/); return m ? [new Date(m[1]).toLocaleTimeString(), m[2]] : ['', line]; };
</script>

<Card class="overflow-hidden">
  <CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
    <div><CardTitle>Box output</CardTitle><CardDescription>Supervisor and robot program output: connections, rejected actions, script errors.</CardDescription></div>
    <div class="flex flex-wrap gap-2">
      <Button variant="outline" size="sm" onclick={load} disabled={loading}>{loading ? 'Loading…' : 'Refresh'}</Button>
      <Button variant="outline" size="sm" onclick={copy}>{copied ? 'Copied' : 'Copy'}</Button>
      <Button variant="outline" size="sm" onclick={download}>Download</Button>
      {#if since}<Button variant="ghost" size="sm" onclick={showAll}>Show all</Button>{/if}
      <Button variant="destructive" size="sm" onclick={clearView}>Clear</Button>
    </div>
  </CardHeader>
  <CardContent class="grid gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <Input class="min-w-48 flex-1" type="search" bind:value={filter} placeholder="Filter lines" ariaLabel="Filter log lines"/>
      <Select class="w-36" bind:value={tail} ariaLabel="Lines to load"><option value="200">Last 200</option><option value="500">Last 500</option><option value="2000">Last 2000</option><option value="5000">Last 5000</option></Select>
      <label class="m-0 flex items-center gap-1.5 font-sans text-sm tracking-normal normal-case"><input type="checkbox" class="accent-primary" bind:checked={errorsOnly}/>Errors only{#if errorCount} <span class="rounded bg-destructive/20 px-1.5 font-mono text-xs text-destructive">{errorCount}</span>{/if}</label>
      <label class="m-0 flex items-center gap-1.5 font-sans text-sm tracking-normal normal-case"><input type="checkbox" class="accent-primary" bind:checked={follow}/>Follow</label>
      <label class="m-0 flex items-center gap-1.5 font-sans text-sm tracking-normal normal-case"><input type="checkbox" class="accent-primary" bind:checked={wrap}/>Wrap</label>
    </div>
    {#if error}<p class="m-0 text-sm text-destructive" role="alert">{error}</p>{/if}
    <pre bind:this={viewer} class="m-0 max-h-96 min-h-40 overflow-auto rounded-md bg-background p-3 font-mono text-[11px] leading-relaxed {wrap ? 'whitespace-pre-wrap' : 'whitespace-pre'}">{#each shown as line}{@const [time, text] = split(line)}<span class="block {isError(line) ? 'text-destructive' : ''}">{#if time}<span class="text-muted-foreground select-none">{time}  </span>{/if}{text}</span>{:else}<span class="text-muted-foreground">{loading ? 'Loading…' : since ? 'Cleared. New output appears here.' : filter || errorsOnly ? 'No lines match.' : 'No output yet. Output appears once your robot runs in a match.'}</span>{/each}</pre>
    <p class="m-0 text-xs text-muted-foreground">{shown.length} of {lines.length} lines{since ? ` · since ${new Date(since).toLocaleTimeString()}` : ''}{follow ? ' · refreshing every 4 s' : ''}. Clear only hides older lines in this browser.</p>
  </CardContent>
</Card>
