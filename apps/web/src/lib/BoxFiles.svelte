<script lang="ts">
  import { api } from './api';
  import type { BoxFile } from './types';
  import { highlightLua } from './luaHighlight';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Button from './components/ui/button.svelte';

  // Read-only browser for /workspace. Editing stays in SSH or the code editor.
  let files = $state<BoxFile[]>([]), open = $state(''), content = $state(''), loading = $state(false), error = $state('');
  async function refresh() {
    loading = true;
    try { files = (await api.boxFiles()).files.sort((a, b) => a.path.localeCompare(b.path)); error = ''; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loading = false; }
  }
  async function view(file: BoxFile) {
    if (file.dir) return;
    open = file.path; content = 'Loading…';
    try { content = (await api.boxFile(file.path)).content; }
    catch (e) { content = e instanceof Error ? e.message : String(e); }
  }
  $effect(() => { void refresh(); });
  const size = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KB` : `${n} B`;
  const depth = (p: string) => p.split('/').length - 1;
  const name = (p: string) => p.split('/').pop() ?? p;
  const total = $derived(files.filter(f => !f.dir).reduce((sum, f) => sum + f.size, 0));
</script>

<Card class="overflow-hidden">
  <CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
    <div><CardTitle>Workspace files</CardTitle><CardDescription><code>/workspace</code> in your box, read-only here. Edit over SSH or in the code editor.</CardDescription></div>
    <Button variant="outline" size="sm" onclick={refresh} disabled={loading}>{loading ? 'Loading…' : 'Refresh'}</Button>
  </CardHeader>
  <CardContent class="grid gap-3 lg:grid-cols-[18rem_minmax(0,1fr)]">
    <div class="max-h-96 overflow-auto rounded-md border border-border">
      {#if error}<p class="m-0 p-3 text-sm text-destructive">{error}</p>{/if}
      <ul class="m-0 grid p-1">
        {#each files as f (f.path)}
          <li class="list-none">
            <button type="button" disabled={f.dir} onclick={() => view(f)} style:padding-left={`${0.5 + depth(f.path)}rem`}
              class="m-0 flex w-full cursor-pointer items-center justify-between gap-2 rounded border-0 bg-transparent py-1 pr-2 text-left font-mono text-xs text-foreground hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent {open === f.path ? 'bg-accent' : ''}">
              <span class="truncate">{f.dir ? '▸ ' : ''}{name(f.path)}{f.dir ? '/' : ''}</span>
              {#if !f.dir}<span class="shrink-0 text-muted-foreground">{size(f.size)}</span>{/if}
            </button>
          </li>
        {:else}<li class="list-none p-3 text-sm text-muted-foreground">{loading ? 'Loading…' : 'No files.'}</li>{/each}
      </ul>
    </div>
    <div class="grid min-w-0 content-start gap-2">
      {#if open}
        <div class="flex items-center justify-between gap-2 text-xs"><code class="truncate">/workspace/{open}</code><span class="text-muted-foreground">{files.find(f => f.path === open)?.modified ? `modified ${new Date((files.find(f => f.path === open)?.modified ?? 0) * 1000).toLocaleString()}` : ''}</span></div>
        <pre class="m-0 max-h-96 overflow-auto rounded-md bg-background p-3 font-mono text-xs leading-relaxed">{#if open.endsWith('.lua')}<code>{@html highlightLua(content)}</code>{:else}<code>{content}</code>{/if}</pre>
      {:else}
        <p class="m-0 text-sm text-muted-foreground">Pick a file to view it. {files.filter(f => !f.dir).length} files · {size(total)} total.</p>
      {/if}
    </div>
  </CardContent>
</Card>
