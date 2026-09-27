<script lang="ts">
  import { api } from './api';
  import type { PlayerLine } from './types';
  import Card from './components/ui/card.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Button from './components/ui/button.svelte';

  // All-time standings from finished Arena V2 matches: the Arena tab ranks
  // by score, All modes by wins then kills. Bots are not listed.
  let { myHandle = '' }: { myHandle?: string } = $props();
  let mode = $state<'arena' | 'all'>('arena');
  let players = $state<PlayerLine[]>([]), loading = $state(true), error = $state('');
  $effect(() => {
    const current = mode;
    loading = true; error = '';
    api.v4Leaderboard(current).then(r => { if (mode === current) players = r.players; }).then(() => loading = false, e => { error = e instanceof Error ? e.message : String(e); loading = false; });
  });
  const kd = (p: PlayerLine) => (p.deaths ? p.kills / p.deaths : p.kills).toFixed(2);
  const medal = ['🥇', '🥈', '🥉'];
</script>

<section class="mx-auto grid max-w-4xl grid-cols-1 gap-5 px-4 py-8 sm:px-8">
  <header class="flex flex-wrap items-end justify-between gap-4">
    <div>
      <p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">HALL OF FAME</p>
      <h1 class="m-0 text-3xl font-semibold tracking-tight">Leaderboard</h1>
      <p class="m-0 mt-1 text-sm text-muted-foreground">Players ranked across finished matches. Play the Arena to climb.</p>
    </div>
    <div class="inline-flex rounded-lg border border-border p-1" role="tablist" aria-label="Ranking">
      {#each [['arena', 'The Arena'], ['all', 'All modes']] as [id, label]}
        <button type="button" role="tab" aria-selected={mode === id} onclick={() => mode = id as 'arena' | 'all'}
          class="m-0 h-8 cursor-pointer rounded-md border-0 px-3 text-sm {mode === id ? 'bg-primary text-primary-foreground' : 'bg-transparent text-muted-foreground hover:text-foreground'}">{label}</button>
      {/each}
    </div>
  </header>

  {#if players.length >= 3}
    <div class="grid grid-cols-3 items-end gap-3" aria-label="Top three">
      {#each [players[1], players[0], players[2]] as p, i}
        {@const place = [2, 1, 3][i]}
        <a href={`#/profile/${p.handle}`} class="podium grid justify-items-center gap-1 rounded-2xl border border-border bg-card p-4 text-center text-foreground no-underline hover:border-primary/60" style:min-height={`${[150, 190, 130][i]}px`} style:animation-delay={`${i * 90}ms`}>
          <span class="text-3xl" aria-hidden="true">{medal[place - 1]}</span>
          <strong class="max-w-full truncate">{p.name}</strong>
          <span class="font-mono text-lg text-primary">{mode === 'arena' ? `${p.score} pts` : `${p.wins} wins`}</span>
          <span class="text-xs text-muted-foreground">{p.kills} kills · {p.matches} matches</span>
        </a>
      {/each}
    </div>
  {/if}

  <Card>
    <CardContent class="p-0">
      <div class="grid grid-cols-[3rem_minmax(0,1fr)_4.5rem_3.5rem_3.5rem_3.5rem] gap-2 border-b border-border px-4 py-2 text-[11px] tracking-wide text-muted-foreground uppercase sm:grid-cols-[3rem_minmax(0,1fr)_5rem_4rem_4rem_4rem_4rem]">
        <span>#</span><span>Player</span><span class="text-right">{mode === 'arena' ? 'Score' : 'Wins'}</span><span class="text-right">Kills</span><span class="text-right">K/D</span><span class="text-right">Games</span><span class="hidden text-right sm:block">Best</span>
      </div>
      {#each players as p, i (p.handle)}
        <a href={`#/profile/${p.handle}`} class="grid grid-cols-[3rem_minmax(0,1fr)_4.5rem_3.5rem_3.5rem_3.5rem] items-center gap-2 border-b border-border/60 px-4 py-2.5 text-sm text-foreground no-underline last:border-0 hover:bg-muted/40 sm:grid-cols-[3rem_minmax(0,1fr)_5rem_4rem_4rem_4rem_4rem] {p.handle === myHandle ? 'bg-accent' : ''}">
          <span class="font-mono text-muted-foreground">{medal[i] ?? `#${i + 1}`}</span>
          <span class="flex min-w-0 items-center gap-2"><span class="grid size-7 shrink-0 place-items-center rounded-full bg-muted text-xs font-semibold">{p.name.slice(0, 2).toUpperCase()}</span><span class="truncate">{p.name}</span>{#if p.handle === myHandle}<span class="rounded bg-primary px-1 text-[10px] font-semibold text-primary-foreground">YOU</span>{/if}</span>
          <span class="text-right font-mono font-semibold">{mode === 'arena' ? p.score : p.wins}</span>
          <span class="text-right font-mono">{p.kills}</span>
          <span class="text-right font-mono text-muted-foreground">{kd(p)}</span>
          <span class="text-right font-mono text-muted-foreground">{p.matches}</span>
          <span class="hidden text-right font-mono text-muted-foreground sm:block">{p.bestScore}</span>
        </a>
      {:else}
        <div class="grid justify-items-start gap-2 p-6">
          <strong>{loading ? 'Loading standings…' : error ? 'Standings could not be loaded' : 'No finished matches yet'}</strong>
          <p class="m-0 text-sm text-muted-foreground">{error || 'Standings fill in as players score. Arena stats are saved every minute.'}</p>
          {#if !loading && !error}<a class="inline-flex" href="#/play"><Button>Play now</Button></a>{/if}
        </div>
      {/each}
    </CardContent>
  </Card>
  <p class="m-0 text-xs text-muted-foreground">Computed from the latest 500 finished matches.</p>
</section>

<style>
  .podium { animation: rise .5s ease both; }
  @keyframes rise { from { opacity: 0; transform: translateY(14px); } }
  @media (prefers-reduced-motion: reduce) { .podium { animation: none; } }
</style>
