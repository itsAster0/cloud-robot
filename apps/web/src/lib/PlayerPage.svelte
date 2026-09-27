<script lang="ts">
  import { api } from './api';
  import type { Match, PlayerLine } from './types';
  import Card from './components/ui/card.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Badge from './components/ui/badge.svelte';
  import { matchHref, matchModeLabel } from './matchNavigation';

  // Public player profile by handle: totals and recent Arena V2 matches.
  let { handle, myHandle = '' }: { handle: string; myHandle?: string } = $props();
  let player = $state<PlayerLine | null>(null), recent = $state<Match[]>([]), loading = $state(true), error = $state('');
  $effect(() => {
    const current = handle;
    loading = true; error = ''; player = null;
    api.v4Player(current).then(r => { if (handle === current) { player = r.player; recent = r.recentMatches; } }).then(() => loading = false, e => { error = e instanceof Error ? e.message : String(e); loading = false; });
  });
  const mine = (m: Match) => m.robotSummaries?.find(s => s.playerId === handle);
</script>

<section class="mx-auto grid max-w-4xl grid-cols-1 gap-5 px-4 py-8 sm:px-8">
  {#if player}
    <header class="flex flex-wrap items-center gap-4">
      <span class="grid size-16 place-items-center rounded-2xl bg-primary text-2xl font-semibold text-primary-foreground">{player.name.slice(0, 2).toUpperCase()}</span>
      <div class="min-w-0">
        <p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">PLAYER · {player.handle}</p>
        <h1 class="m-0 truncate text-3xl font-semibold tracking-tight">{player.name}{#if handle === myHandle} <Badge variant="live">You</Badge>{/if}</h1>
      </div>
    </header>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {#each [['Arena score', player.score], ['Wins', player.wins], ['Kills', player.kills], ['K/D', (player.deaths ? player.kills / player.deaths : player.kills).toFixed(2)], ['Matches', player.matches], ['Deaths', player.deaths], ['Damage dealt', player.damageDealt], ['Best score', player.bestScore]] as [label, value]}
        <Card><CardContent class="grid gap-1 p-4"><span class="text-xs text-muted-foreground">{label}</span><strong class="font-mono text-2xl">{value}</strong></CardContent></Card>
      {/each}
    </div>
    <h2 class="m-0 text-lg font-semibold">Recent matches</h2>
    <div class="grid gap-2">
      {#each recent as m (m.matchId)}
        {@const me = mine(m)}
        <a class="flex items-center justify-between gap-3 rounded-xl border border-border bg-card px-4 py-3 text-foreground no-underline hover:border-primary/50" href={matchHref(m)}>
          <span class="min-w-0"><strong>{matchModeLabel(m.mode)}</strong>{#if me && m.winnerTeam === me.team} <Badge variant="live">Won</Badge>{/if}<span class="block text-xs text-muted-foreground">{new Date(m.createdAt).toLocaleString()}</span></span>
          <span class="shrink-0 font-mono text-sm text-muted-foreground">{me ? `${me.score ?? 0} pts · ${me.kills ?? 0}K ${me.deaths ?? 0}D` : ''}</span>
        </a>
      {/each}
    </div>
  {:else}
    <Card><CardContent class="grid justify-items-start gap-2 p-6">
      <strong>{loading ? 'Loading player…' : 'No stats yet'}</strong>
      <p class="m-0 text-sm text-muted-foreground">{loading ? '' : handle === myHandle ? 'Your stats appear after your first match with a result. Play the Arena: sessions finish every 30 minutes.' : error}</p>
      {#if !loading}<a href="#/play">Play now →</a>{/if}
    </CardContent></Card>
  {/if}
</section>
