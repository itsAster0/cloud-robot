<script lang="ts">
  import { api, request } from './api';
  import type { Match } from './types';
  import Button from './components/ui/button.svelte';

  let { signedIn, onSignIn }: { signedIn: boolean; onSignIn?: () => void } = $props();

  // Each personality is a shipped strategy plus a legal 60-point build that
  // suits it, so a newcomer never has to understand loadouts to play.
  const robots = [
    { id: 'v4', name: 'Balanced', glyph: '◆', tag: 'Good all-rounder', text: 'Fights at range, takes cover when hurt, grabs loot, and hunts enemies it has seen.', loadout: { chassis: 'generalist', weapon: 'plasma', modules: ['reinforced_plating', 'cooling_system'], utilities: [] } },
    { id: 'assault', name: 'Brawler', glyph: '✦', tag: 'Up close and loud', text: 'Charges in with a shotgun, dashes to close the gap, and drops mines when backing off.', loadout: { chassis: 'heavy', weapon: 'shotgun', modules: ['reinforced_plating'], utilities: ['mine_dispenser'] } },
    { id: 'sniper', name: 'Sniper', glyph: '◎', tag: 'Patient and precise', text: 'Keeps its distance with a railgun and takes steady long shots.', loadout: { chassis: 'generalist', weapon: 'railgun', modules: ['optics', 'cooling_system'], utilities: [] } },
    { id: 'scout', name: 'Scout', glyph: '➤', tag: 'Fast and sneaky', text: 'Moves quickly, scans constantly, rides transit links, and slips away when hurt.', loadout: { chassis: 'scout', weapon: 'machine_gun', modules: ['optics', 'mobility_tuning'], utilities: ['cloak_emitter'] } },
    { id: 'sentinel', name: 'Guardian', glyph: '▣', tag: 'Holds its ground', text: 'Guards the centre of the safe zone with a cannon and mines the approaches.', loadout: { chassis: 'heavy', weapon: 'cannon', modules: [], utilities: ['mine_dispenser'] } },
    { id: 'scavenger', name: 'Scavenger', glyph: '✚', tag: 'Loots everything', text: 'Races for weapons and supplies first, then fights with whatever it found.', loadout: { chassis: 'scout', weapon: 'machine_gun', modules: ['mobility_tuning'], utilities: [] } },
  ];
  // `config: null` is the always-on arena: no lobby, no start, respawns.
  const fights: { id: string; name: string; detail: string; minutes: number; config: Record<string, unknown> | null; featured?: boolean }[] = [
    { id: 'arena', name: 'The Arena', detail: 'Drop into the always-on arena with everyone else. Respawn when destroyed.', minutes: 0, config: null, featured: true },
    { id: 'duel', name: 'Warm-up', detail: 'You vs 1 bot', minutes: 3, config: { mode: 'sandbox', capacity: 2, width: 2400, height: 1500, durationSeconds: 180, siteCount: 4, coverPerSite: 8, lootPerSite: 16 } },
    { id: 'brawl', name: 'Brawl', detail: 'You vs 5 bots', minutes: 3, config: { mode: 'sandbox', capacity: 6, width: 2400, height: 1500, durationSeconds: 180, siteCount: 4, coverPerSite: 8, lootPerSite: 16 } },
    { id: 'royale', name: 'Battle royale', detail: 'You vs 15 bots on a big map', minutes: 5, config: { mode: 'br-solo', capacity: 16, width: 12000, height: 7500, durationSeconds: 300, siteCount: 16, coverPerSite: 8, lootPerSite: 16 } },
  ];

  let robot = $state(robots[0]), fight = $state(fights[0]);
  let arenaPlayers = $state<number | null>(null);
  $effect(() => { api.getArena().then(a => arenaPlayers = a.players).catch(() => arenaPlayers = null); });
  const stepNames = ['Getting your robot box ready', 'Loading the robot’s brain', 'Fitting its parts', 'Building the arena', 'Sending your robot in', 'Waiting for your robot to connect', 'Starting the match'];
  const arenaSteps = ['Getting your robot box ready', 'Loading the robot’s brain', 'Fitting its parts', 'Finding the arena', 'Dropping your robot in'];
  let steps = $derived(fight.config ? stepNames : arenaSteps);
  let running = $state(false), current = $state(-1), failure = $state('');

  const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));
  // Turn API wording into something a first-time player can act on.
  function explain(message: string) {
    if (/sign in/i.test(message)) return 'Please sign in first.';
    if (/still starting|running SSH box/i.test(message)) return 'Your robot box is still starting. Give it a few seconds and try again.';
    if (/unavailable|Failed to fetch|502|503/i.test(message)) return 'The arena server is not reachable right now. Check that the stack is running, then try again.';
    return message;
  }

  async function play() {
    running = true; failure = ''; current = 0;
    try {
      let box = await api.ensureBox();
      for (let i = 0; i < 40 && box.status !== 'running'; i++) { await sleep(1000); box = await api.getBox(); }
      if (box.status !== 'running') throw new Error('Your robot box did not start. Try again in a moment.');
      // A robot still bound to an old match would block registration.
      if (box.activeMatchId) await api.releaseBox().catch(() => {});

      current = 1; await api.writeBoxMain(robot.id);
      current = 2; await request('/api/v4/me/loadout', { method: 'PUT', body: JSON.stringify(robot.loadout) });
      current = 3;
      if (!fight.config) {
        let arena = await api.getArena().catch(() => null);
        for (let i = 0; i < 20 && arena?.match.status !== 'running'; i++) { await sleep(1500); arena = await api.getArena().catch(() => null); }
        if (!arena || arena.match.status !== 'running') throw new Error('The arena is starting up. Try again in a few seconds.');
        current = 4;
        await request(`/api/matches/${arena.match.matchId}/robots`, { method: 'POST', body: JSON.stringify({ displayName: robot.name, runtime: 'lua5.4', startCommand: 'lua main.lua', sdkVersion: '0.4.0', loadout: robot.loadout }) });
        current = 5;
        try { sessionStorage.setItem('robot-arena:welcome', arena.match.matchId); } catch { /* banner is optional */ }
        window.location.hash = `#/v2/${arena.match.matchId}`;
        return;
      }
      const seed = 1 + Math.floor(Math.random() * 999998);
      const match = await request<Match>('/api/v4/matches', { method: 'POST', body: JSON.stringify({ ...fight.config, seed, liveEdit: false }) });

      current = 4;
      await request(`/api/matches/${match.matchId}/robots`, { method: 'POST', body: JSON.stringify({ displayName: robot.name, team: 'squad-00', runtime: 'lua5.4', startCommand: 'lua main.lua', sdkVersion: '0.4.0', loadout: robot.loadout }) });

      // The server refuses to start until the robot's agent has connected.
      current = 5;
      let started = false, lastError = '';
      for (let i = 0; i < 45 && !started; i++) {
        try { await api.startMatch(match.matchId); started = true; }
        catch (e) { lastError = e instanceof Error ? e.message : String(e); await sleep(1000); }
      }
      if (!started) throw new Error(`Your robot did not connect in time (${lastError}).`);
      current = 6; await sleep(400);
      current = 7;
      try { sessionStorage.setItem('robot-arena:welcome', match.matchId); } catch { /* banner is optional */ }
      window.location.hash = `#/v2/${match.matchId}`;
    } catch (e) {
      failure = explain(e instanceof Error ? e.message : String(e));
      running = false;
    }
  }
</script>

<section class="mx-auto grid max-w-5xl grid-cols-1 gap-8 px-4 py-10 sm:px-8">
  <header class="grid gap-2 text-center">
    <h1 class="m-0 text-3xl font-semibold tracking-tight sm:text-4xl">Pick a robot. Press play.</h1>
    <p class="m-0 text-muted-foreground">Your robot is driven by a small program. Choose a personality now; you can read and change its code later.</p>
  </header>

  {#if !signedIn}
    <div class="mx-auto grid max-w-md justify-items-center gap-3 rounded-2xl border border-border bg-card p-8 text-center">
      <p class="m-0 text-lg font-semibold">Sign in to get your own robot</p>
      <p class="m-0 text-sm text-muted-foreground">It takes a few seconds. Anyone can watch matches without an account.</p>
      <div class="flex flex-wrap justify-center gap-2"><Button size="lg" onclick={onSignIn}>Sign in to play</Button><a class="inline-flex h-11 items-center px-4 text-sm" href="#/matches">Watch matches instead</a></div>
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-3">
      <h2 class="m-0 flex items-center gap-2 text-lg font-semibold"><span class="grid size-7 place-items-center rounded-full bg-primary text-sm text-primary-foreground">1</span>Choose your robot</h2>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3" role="radiogroup" aria-label="Robot personality">
        {#each robots as option (option.id)}
          <button type="button" role="radio" aria-checked={robot.id === option.id} disabled={running} onclick={() => robot = option}
            class="qp-card group m-0 grid cursor-pointer gap-2 rounded-2xl border p-4 text-left text-foreground {robot.id === option.id ? 'border-primary bg-accent shadow-[0_0_0_1px_var(--color-primary)]' : 'border-border bg-card hover:border-input'}">
            <span class="flex items-center gap-3">
              <span class="grid size-11 place-items-center rounded-xl text-xl transition-transform group-hover:scale-110 {robot.id === option.id ? 'bg-primary text-primary-foreground' : 'bg-muted text-primary'}" aria-hidden="true">{option.glyph}</span>
              <span><span class="block font-semibold">{option.name}</span><span class="block text-xs text-muted-foreground">{option.tag}</span></span>
            </span>
            <span class="text-sm text-muted-foreground">{option.text}</span>
          </button>
        {/each}
      </div>
    </div>

    <div class="grid grid-cols-1 gap-3">
      <h2 class="m-0 flex items-center gap-2 text-lg font-semibold"><span class="grid size-7 place-items-center rounded-full bg-primary text-sm text-primary-foreground">2</span>Choose a fight</h2>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4" role="radiogroup" aria-label="Fight size">
        {#each fights as option (option.id)}
          <button type="button" role="radio" aria-checked={fight.id === option.id} disabled={running} onclick={() => fight = option}
            class="qp-card m-0 grid cursor-pointer gap-1 rounded-2xl border p-4 text-left text-foreground {fight.id === option.id ? 'border-primary bg-accent shadow-[0_0_0_1px_var(--color-primary)]' : 'border-border bg-card hover:border-input'}">
            <span class="flex items-center gap-2 font-semibold">{option.name}{#if option.featured}<span class="rounded-full bg-primary px-2 py-0.5 text-[10px] text-primary-foreground">LIVE</span>{/if}</span><span class="text-sm text-muted-foreground">{option.detail}</span><span class="text-xs text-muted-foreground">{option.config ? `About ${option.minutes} minutes` : arenaPlayers != null ? `${arenaPlayers} ${arenaPlayers === 1 ? 'player' : 'players'} in now · stay as long as you like` : 'Stay as long as you like'}</span>
          </button>
        {/each}
      </div>
    </div>

    <div class="grid justify-items-center gap-4">
      {#if !running}
        <button type="button" onclick={play} class="qp-go m-0 cursor-pointer rounded-2xl border-0 bg-primary px-10 py-4 text-lg font-semibold text-primary-foreground">{fight.config ? "Start the match ▶" : "Enter the Arena ▶"}</button>
        <p class="m-0 text-xs text-muted-foreground">{robot.name} · {fight.name}. Everything is set up for you.</p>
      {/if}
      {#if running || failure}
        <ol class="m-0 grid w-full max-w-md gap-2 rounded-2xl border border-border bg-card p-5 pl-5" aria-live="polite">
          {#each steps as name, index}
            <li class="flex list-none items-center gap-3 text-sm transition-opacity" class:opacity-40={index > current}>
              <span class="grid size-6 shrink-0 place-items-center rounded-full text-xs {index < current ? 'bg-primary text-primary-foreground' : index === current && !failure ? 'border-2 border-primary border-t-transparent animate-spin' : index === current && failure ? 'bg-destructive text-white' : 'border border-border'}" aria-hidden="true">{index < current ? '✓' : index === current && failure ? '!' : ''}</span>
              <span class:font-medium={index === current}>{name}</span>
            </li>
          {/each}
        </ol>
      {/if}
      {#if failure}
        <div class="grid max-w-md justify-items-center gap-2 text-center" role="alert"><p class="m-0 text-sm text-destructive">{failure}</p><Button onclick={play}>Try again</Button></div>
      {/if}
    </div>

    <p class="m-0 text-center text-sm text-muted-foreground">Want to write your own robot? Open <a href="#/workspace">My robot</a> to edit its code, or read the <a href="#/docs/sdk">quick guide</a>.</p>
  {/if}
</section>

<style>
  .qp-card { transition: transform .18s ease, border-color .18s ease, background-color .18s ease, box-shadow .18s ease; }
  .qp-card:hover:not(:disabled) { transform: translateY(-2px); }
  .qp-go { transition: transform .15s ease, filter .15s ease; box-shadow: 0 10px 40px -12px var(--color-primary); animation: qp-pulse 2.4s ease-in-out infinite; }
  .qp-go:hover { transform: scale(1.04); filter: brightness(1.08); }
  @keyframes qp-pulse { 50% { box-shadow: 0 10px 50px -6px var(--color-primary); } }
  @media (prefers-reduced-motion: reduce) { .qp-go { animation: none; } .qp-card:hover:not(:disabled) { transform: none; } }
</style>
