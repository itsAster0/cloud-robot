<script lang="ts">
  import type { User } from '@workos-inc/authkit-js';
  import { api, type ListedMatch } from './api';
  import type { AdminStatus, CloudStatus, Match, PlayerStats, QueueStatus, Replay, RobotBox } from './types';
  import type { Route } from './router';
  import DocsPage from './DocsPage.svelte';
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Input from './components/ui/input.svelte';
  import Select from './components/ui/select.svelte';
  import Badge from './components/ui/badge.svelte';
  import { matchHref, matchModeLabel, matchRosterLabel } from './matchNavigation';

  interface Props { route: Route; user: User | null; match: Match | null; cloud: CloudStatus | null; box?: RobotBox | null; onSignIn: () => void; onCreateMatch: () => void; onReleaseBox: () => Promise<void>; }
  let { route, user, match, cloud, box = null, onSignIn, onCreateMatch, onReleaseBox }: Props = $props();
  let mode = $state('duel'), arenaSize = $state('medium'), queueing = $state(false), filter = $state('all');
  let botPersonality = $state('aggressive'), practiceRegen = $state(false), practiceRamming = $state(false), soloBots = $state(3);
  let soloBotCount = $derived(Number(soloBots));
  let displayName = $state('Robot'), startCommand = $state('lua main.lua');
  let reducedMotion = $state(false);
  let customWidth = $state(800), customHeight = $state(500), mapName = $state('Untitled arena');
  let matches = $state<ListedMatch[]>([]), profile = $state<PlayerStats | null>(null), recentMatches = $state<Match[]>([]);
  let leaderboard = $state<{ rank: number; player: PlayerStats; rating: number }[]>([]);
  let queue = $state<QueueStatus>({ status: 'idle' }), replay = $state<Replay | null>(null), admin = $state<AdminStatus | null>(null);
  let search = $state('');
  let loading = $state(false), pageError = $state(''), releasing = $state(false);
  let blockedByActiveRobot = $derived(pageError.includes('box already has an active robot'));

  try { reducedMotion = localStorage.getItem('arena-reduced-motion') === 'true'; } catch { /* storage unavailable */ }
  function savePreferences() {
    try { localStorage.setItem('arena-reduced-motion', String(reducedMotion)); } catch { /* preference stays for this page only */ }
  }
  function requireUser(action: () => void) { if (user) action(); else onSignIn(); }
  let title = $derived(route.name === 'profile' ? `${route.parameter ?? 'Player'} profile` : route.name === 'sdk' || route.name === 'api-docs' ? 'documentation' : route.name.replace('-', ' '));
  let visibleMatches = $derived(matches.filter((entry) =>
    (filter === 'all' || entry.status === filter) &&
    `${entry.matchId} ${matchModeLabel(entry.mode)} ${(entry.robots ?? []).map(robot => robot.displayName).join(' ')}`.toLowerCase().includes(search.trim().toLowerCase())
  ));

  async function loadPage() {
    loading = true; pageError = '';
    try {
      if (route.name === 'matches') matches = (await api.listMatches(undefined, 50)).matches;
      if (route.name === 'spectate') matches = (await api.listMatches('running', 50)).matches;
      if (route.name === 'profile' && route.parameter) ({ profile, recentMatches } = await api.getProfile(route.parameter));
      if (route.name === 'leaderboard') leaderboard = (await api.getLeaderboard('duel', 50)).entries;
      if (route.name === 'match-detail' && route.parameter) replay = await api.getReplay(route.parameter);
      if (route.name === 'play' && user) { queue = await api.queueStatus(); queueing = queue.status !== 'idle'; openMatchedQueue(); }
      if (route.name === 'admin' && user) admin = await api.adminStatus();
    } catch (failure) { pageError = failure instanceof Error ? failure.message : String(failure); }
    finally { loading = false; }
  }

  function openMatchedQueue() {
    const matchId = queue.match?.matchId ?? queue.matchId;
    if (queue.status !== 'matched' || !matchId) return;
    // A queue entry that outlived its match (older server, restart races)
    // must never trap /play in a redirect loop back to a finished match.
    if (match?.matchId === matchId && (match.status === 'finished' || match.status === 'failed')) return;
    window.location.hash = `/match/${matchId}`;
  }
  async function refreshQueue() { if (!user || !queueing) return; try { queue = await api.queueStatus(); openMatchedQueue(); } catch (failure) { pageError = failure instanceof Error ? failure.message : String(failure); } }
  async function toggleQueue() {
    pageError = '';
    try {
      if (queueing) queue = await api.leaveQueue();
      else queue = await api.joinQueue({ displayName: displayName.trim(), mode: 'duel', runtime: 'lua5.4', startCommand: startCommand.trim() });
      queueing = queue.status !== 'idle'; openMatchedQueue();
    } catch (failure) { pageError = failure instanceof Error ? failure.message : String(failure); }
  }
  async function startLobby() {
    pageError = '';
    const sizes: Record<string, [number, number]> = { small: [600, 400], medium: [800, 500], large: [1200, 800] };
    try {
      const created = await api.createMatch({
        mode,
        practice: true,
        bots: mode === 'solo' ? soloBotCount : undefined,
        botDifficulty: 'fighter',
        arenaWidth: mode === 'squad' ? undefined : sizes[arenaSize][0],
        arenaHeight: mode === 'squad' ? undefined : sizes[arenaSize][1],
        regenPerTick: practiceRegen ? 1 : 0,
        rammingDamage: practiceRamming,
        botPersonality,
      });
      window.location.hash = `/match/${created.matchId}`;
    } catch (failure) { pageError = failure instanceof Error ? failure.message : String(failure); }
  }

  // Same escape hatch as the global FAULT banner: release the box from the
  // match that blocks queueing, then refresh queue state so a retry works.
  async function releaseActiveMatch() {
    pageError = '';
    releasing = true;
    try {
      await onReleaseBox();
      if (route.name === 'play' && user) { queue = await api.queueStatus(); queueing = queue.status !== 'idle'; }
    } catch (failure) { pageError = failure instanceof Error ? failure.message : String(failure); }
    finally { releasing = false; }
  }

  $effect(() => { route.name; route.parameter; user; if (user?.firstName && displayName === 'Robot') displayName = user.firstName; void loadPage(); });
  $effect(() => { if (!queueing) return; const timer = setInterval(() => void refreshQueue(), 2000); return () => clearInterval(timer); });
</script>

<svelte:head><title>{title.charAt(0).toUpperCase() + title.slice(1)} · Robot Arena</title></svelte:head>
{#if pageError}<div class="portal-alert" role="alert">{pageError}{#if blockedByActiveRobot}<button onclick={releaseActiveMatch} disabled={releasing}>{releasing ? 'RELEASING…' : 'EXIT ACTIVE MATCH'}</button>{/if}</div>{/if}

{#if route.name === 'home'}
  <section class="portal hero-page">
    <div class="hero-copy"><div class="eyebrow">PROGRAMMABLE ROBOT COMBAT</div><h1>Write tactics.<br /><em>Watch them fight.</em></h1><p>Deploy Lua into a persistent SSH box. Server runs deterministic matches. Anyone can watch.</p><div class="hero-actions"><button class="deploy" onclick={() => requireUser(onCreateMatch)}>{user ? 'PLAY NOW' : 'SIGN IN TO PLAY'} <span>↗</span></button><a class="secondary-action" href="#/spectate">WATCH LIVE</a></div></div>
    <article class="feature-card"><div class="eyebrow">{match?.status === 'running' ? 'LIVE NOW' : 'FEATURED MATCH'}</div>{#if match}<h2>{match.mode} // {match.mapId ?? 'Open Field'}</h2><p>{(match.robots ?? []).map((robot) => robot.displayName).join(' vs ') || 'Lobby forming'}</p><a href={`#/match/${match.matchId}`}>OPEN MATCH</a>{:else}<h2>No match live</h2><p>Create one or open Spectate to browse active matches.</p><a href="#/play">PRACTICE VS BOTS</a>{/if}</article>
    <section class="quick-grid" aria-label="How to start"><article><b>01</b><h3>Provision box</h3><p>One persistent, resource-limited workspace per account.</p></article><article><b>02</b><h3>Deploy Lua</h3><p>Edit <code>/workspace/main.lua</code> over SSH.</p></article><article><b>03</b><h3>Enter arena</h3><p>Agent sends intent. Server owns state and scoring.</p></article></section>
  </section>
{:else if route.name === 'play'}
  <section class="portal narrow-page"><div class="eyebrow">MATCHMAKING</div><h1>Enter arena.</h1><p class="page-lede">Join the duel queue, field a 5v5 squad against ten bots, or go solo in a free-for-all sandbox. Queue fills with a bot when another player does not arrive.</p>
    {#if box?.activeMatchId}
      <div class="box-match-note" data-tone="ok"><span>IN MATCH</span><div>Your box is committed to <a href={`#/match/${box.activeMatchId}`}>match {box.activeMatchId.slice(0, 8)}</a>. Queueing stays blocked until it ends. <a href={`#/match/${box.activeMatchId}`}>RESUME MATCH →</a> <button onclick={releaseActiveMatch} disabled={releasing}>{releasing ? 'RELEASING…' : 'EXIT MATCH'}</button></div></div>
    {/if}
    <div class="form-grid"><label>Mode<select bind:value={mode} disabled={queueing}><option value="duel">Duel queue</option><option value="squad">Squad 5v5 vs bots</option><option value="solo">Solo free-for-all</option></select></label>{#if mode !== 'duel'}<label>Arena size<select bind:value={arenaSize} disabled={queueing || mode === 'squad'}><option value="small">Small · 600 × 400</option><option value="medium">Medium · 800 × 500</option><option value="large">Large · 1200 × 800</option></select></label>{/if}{#if mode === 'squad'}<label>Squad roster<input value="10 bots · 5 per side" disabled /></label>{/if}{#if mode === 'solo'}<label>Bot opponents<select bind:value={soloBots} disabled={queueing}>{#each Array(8) as _, count}<option value={count}>{count === 0 ? 'No bots · sandbox' : `${count} bot${count > 1 ? 's' : ''}`}</option>{/each}</select></label>{/if}{#if mode !== 'duel'}<label>Bot personality<select bind:value={botPersonality} disabled={queueing}><option value="aggressive">Aggressive</option><option value="evasive">Evasive</option><option value="camper">Camper</option><option value="mixed">Mixed roster</option></select></label><label>Regen after 5 s calm<input type="checkbox" bind:checked={practiceRegen} disabled={queueing} /></label><label>Ramming damage<input type="checkbox" bind:checked={practiceRamming} disabled={queueing} /></label>{/if}<label>Robot name<input bind:value={displayName} maxlength="32" disabled={queueing} /></label><label>Start command<input bind:value={startCommand} maxlength="256" disabled={queueing} /></label></div>
    {#if mode === 'squad'}<p class="page-lede">The lobby opens with five bots per side. Registering on a team replaces one of its bots; both teams keep five robots.</p>{/if}
    {#if user}<button class="deploy" onclick={mode === 'duel' ? toggleQueue : startLobby}>{queueing ? `LEAVE QUEUE · ${queue.status.toUpperCase()}` : mode === 'squad' ? 'CREATE SQUAD LOBBY' : mode === 'solo' ? 'CREATE SOLO ARENA' : 'JOIN DUEL QUEUE'} <span>↗</span></button>{:else}<div class="auth-gate"><h2>Sign in to play</h2><p>Watching, profiles, results, and docs stay public.</p><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}
    <div class="roster-preview">{#if mode === 'squad'}<div><span>RED // 5</span><strong>{displayName || user?.firstName || 'You'} + 4 bots</strong></div><div><span>BLUE // 5</span><strong>5 bots</strong></div>{:else if mode === 'solo'}<div><span>FREE-FOR-ALL</span><strong>{displayName || user?.firstName || 'You'}</strong></div><div><span>OPPONENTS</span><strong>{soloBotCount === 0 ? 'Empty sandbox' : `${soloBotCount} bot${soloBotCount > 1 ? 's' : ''}`}</strong></div>{:else}<div><span>RED // R</span><strong>{displayName || user?.firstName || 'You'}</strong></div><div><span>BLUE // B</span><strong>{queue.status === 'waiting' ? 'Searching…' : 'Waiting'}</strong></div>{/if}</div>
  </section>
{:else if route.name === 'matches' || route.name === 'spectate'}
  <section class="mx-auto grid max-w-[1100px] gap-5 px-4 py-8 sm:px-8 grid-cols-1">
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">{route.name === 'spectate' ? 'WATCH AND LEARN' : 'MATCH LIBRARY'}</p>
        <h1 class="m-0 text-3xl font-semibold tracking-tight">{route.name === 'spectate' ? 'Live matches' : 'Matches & replays'}</h1>
        <p class="m-0 mt-1 text-sm text-muted-foreground">{route.name === 'spectate' ? 'Watch robot strategies play out. Public viewing is open to everyone.' : 'Revisit a run, inspect its outcome, and take what you learn back to your code.'}</p>
      </div>
      <a class="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground no-underline hover:bg-primary/90" href="#/workspace/matches">Create a match</a>
    </header>
    <Card>
      <CardContent class="flex flex-wrap items-center gap-2 p-3">
        <Input class="min-w-56 flex-1" type="search" bind:value={search} placeholder="Search robot, mode, or match ID" ariaLabel="Find a match"/>
        {#if route.name === 'matches'}<Select class="w-40" bind:value={filter} ariaLabel="Status"><option value="all">All statuses</option><option value="lobby">Lobby</option><option value="queued">Queued</option><option value="running">Live</option><option value="finished">Finished</option><option value="failed">Failed</option></Select>{/if}
        <Button variant="secondary" onclick={loadPage} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</Button>
      </CardContent>
    </Card>
    <p class="m-0 text-xs text-muted-foreground" aria-live="polite">{loading ? 'Loading matches…' : `${visibleMatches.length} ${visibleMatches.length === 1 ? 'match' : 'matches'} shown · latest 50 records`}</p>
    <div class="grid gap-2 grid-cols-1">
      {#each visibleMatches as listed (listed.matchId)}
        <a class="group flex items-center justify-between gap-4 rounded-xl border border-border bg-card px-4 py-3 text-foreground no-underline transition-colors hover:border-primary/50 hover:bg-muted/40" href={matchHref(listed)}>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <strong class="text-base font-semibold">{matchModeLabel(listed.mode)}</strong>
              <Badge variant={listed.status === 'running' ? 'live' : listed.status === 'failed' ? 'danger' : listed.status === 'finished' ? 'default' : 'muted'}>{listed.status === 'running' ? 'Live' : listed.status}</Badge>
              <span class="font-mono text-[11px] text-muted-foreground">{listed.engineVersion === 4 ? 'Arena V2' : 'Classic'} · {listed.matchId.slice(0, 8)}</span>
            </div>
            <p class="m-0 mt-1 truncate text-sm text-muted-foreground">{matchRosterLabel(listed)}</p>
            <p class="m-0 text-xs text-muted-foreground">{new Date(listed.createdAt).toLocaleString()}{#if listed.status === 'running'} · {listed.viewers ?? 0} watching{/if}</p>
          </div>
          <span class="shrink-0 text-sm font-medium text-primary">{listed.status === 'finished' ? 'View results →' : listed.status === 'lobby' ? 'Open lobby →' : listed.status === 'failed' ? 'View match →' : 'Watch →'}</span>
        </a>
      {:else}
        <Card><CardContent class="grid justify-items-start gap-2 p-6 grid-cols-1">
          <strong>{loading ? 'Loading matches…' : pageError ? 'Matches could not be loaded' : search || filter !== 'all' ? 'No matches found' : route.name === 'spectate' ? 'No matches are live' : 'Your next experiment starts here'}</strong>
          <p class="m-0 text-sm text-muted-foreground">{pageError ? 'Check the connection and refresh to try again.' : search || filter !== 'all' ? 'Try another search or status filter.' : 'Create a sandbox to test your robot, or start a match with game bots.'}</p>
          <a class="text-sm" href="#/workspace/matches">Open match setup →</a>
        </CardContent></Card>
      {/each}
    </div>
  </section>
{:else if route.name === 'match-detail'}
  <section class="portal narrow-page"><div class="eyebrow">MATCH RECAP</div><h1>{match?.winnerTeam ? `${match.winnerTeam.toUpperCase()} wins.` : 'Result unavailable.'}</h1><div class="recap-grid"><div><span>DURATION</span><strong>{match?.startedAt && match?.finishedAt ? `${Math.round((Date.parse(match.finishedAt) - Date.parse(match.startedAt)) / 1000)} s` : '--'}</strong></div><div><span>MAP</span><strong>{match?.mapId ?? 'open-field'}</strong></div><div><span>MODE</span><strong>{match?.mode ?? '--'}</strong></div><div><span>REPLAY</span><strong>{replay?.complete ? 'COMPLETE' : `${replay?.events.length ?? 0} SUMMARY EVENTS`}</strong></div></div>{#each match?.robotSummaries ?? [] as robot}<article class="stat-row"><b class={robot.team === 'red' || robot.team === 'blue' ? robot.team : 'solo'}>{robot.team === 'red' ? 'R' : robot.team === 'blue' ? 'B' : 'S'}</b><strong>{robot.name}</strong><span>{robot.kills ?? 0} kills</span><span>{robot.damageDealt ?? 0} dealt</span><span>{robot.itemsPickedUp ?? 0} items</span></article>{/each}{#if replay?.events.length}<div class="replay-feed">{#each replay.events as event}<div><time>T{event.tick}</time><strong>{event.type.replace(/_/g, ' ')}</strong><span>{event.message ?? (event.damage ? `${event.damage} damage` : event.robotId)}</span></div>{/each}</div>{/if}<a class="secondary-action" href={`#/match/${route.parameter}`}>OPEN ARENA VIEW</a></section>
{:else if route.name === 'profile'}
  <section class="portal narrow-page"><div class="eyebrow">PLAYER PROFILE</div><h1>{profile?.handle ?? route.parameter}</h1>{#if profile}<div class="profile-card"><div class="profile-mark">{profile.handle.slice(0, 2).toUpperCase()}</div><div><span>PLAYER ID</span><strong>{profile.playerId.slice(0, 12)}</strong><p>Stats updated {new Date(profile.updatedAt).toLocaleString()}.</p></div></div><div class="recap-grid"><div><span>DUEL RATING</span><strong>{profile.ratings.duel ?? 1000}</strong></div><div><span>WINS</span><strong>{profile.wins}</strong></div><div><span>LOSSES / DRAWS</span><strong>{profile.losses} / {profile.draws}</strong></div><div><span>DAMAGE DEALT</span><strong>{profile.damageDealt}</strong></div></div>{#each recentMatches as recent}<a class="match-card" href={matchHref(recent)}><div><strong>{recent.mode} · {recent.mapId}</strong><p>{new Date(recent.createdAt).toLocaleString()}</p></div><strong>{recent.winnerTeam?.toUpperCase() ?? recent.status.toUpperCase()} ›</strong></a>{/each}{:else if !loading}<div class="empty-state"><h2>Profile not found</h2><p>No recorded stats for this handle.</p></div>{/if}</section>
{:else if route.name === 'leaderboard'}
  <section class="portal"><div class="eyebrow">RANKED // DUEL</div><h1>Leaderboard.</h1><div class="ladder"><div class="ladder-head"><span>RANK</span><span>PLAYER</span><span>RATING</span><span>W/L</span></div>{#each leaderboard as entry}<a class="ladder-row" href={`#/profile/${encodeURIComponent(entry.player.handle)}`}><span>#{entry.rank}</span><strong>{entry.player.handle}</strong><span>{entry.rating}</span><span>{entry.player.wins}/{entry.player.losses}</span></a>{:else}<div class="empty-state"><h2>{loading ? 'Loading ladder…' : 'No ranked results'}</h2><p>Completed duel results will fill this ladder.</p></div>{/each}</div></section>
{:else if route.name === 'create'}
  <section class="portal"><div class="eyebrow">WORKSHOP</div><h1>Create arena.</h1>{#if user}<div class="workshop-grid"><article class="console-card"><label>Map name<input bind:value={mapName} maxlength="48" /></label><div class="form-grid"><label>Width<input type="number" min="400" max="1600" bind:value={customWidth} /></label><label>Height<input type="number" min="300" max="1000" bind:value={customHeight} /></label></div><button class="secondary-action" disabled>EXPORT JSON · COMING LATER</button></article><article class="map-preview" style={`aspect-ratio: ${customWidth}/${customHeight}`}><span>{mapName}</span><i>CUSTOM {customWidth} × {customHeight}</i></article></div>{:else}<div class="auth-gate"><h2>Sign in to use Workshop</h2><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}</section>
{:else if route.name === 'settings'}
  <section class="mx-auto grid max-w-2xl gap-5 px-4 py-8 sm:px-8 grid-cols-1">
    <header><p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">ACCOUNT</p><h1 class="m-0 text-3xl font-semibold tracking-tight">Settings</h1></header>
    {#if user}
      <Card><CardContent class="flex flex-wrap items-center justify-between gap-3 p-5">
        <div class="min-w-0"><p class="m-0 font-semibold">{[user.firstName, user.lastName].filter(Boolean).join(' ') || 'Signed in'}</p><p class="m-0 truncate text-sm text-muted-foreground">{user.email}</p></div>
        <Badge variant="live">Signed in with WorkOS</Badge>
      </CardContent></Card>
      <Card><CardContent class="grid gap-2 p-5 sm:grid-cols-3 grid-cols-1">
        {#each [['#/box', 'Robot box', 'SSH key, resources, output'], ['#/workspace/build', 'Loadout', 'Your 60-point build'], ['#/workspace', 'Code', 'Edit main.lua']] as [href, name, detail]}
          <a class="grid gap-0.5 rounded-lg border border-border bg-background p-3 text-foreground no-underline hover:border-primary/60 grid-cols-1" {href}><strong class="text-sm">{name}</strong><span class="text-xs text-muted-foreground">{detail}</span></a>
        {/each}
      </CardContent></Card>
      <Card><CardContent class="flex items-center justify-between gap-4 p-5">
        <div><p class="m-0 font-medium">Reduced motion</p><p class="m-0 text-sm text-muted-foreground">Snap the arena camera instead of easing, and skip robot interpolation. Saved in this browser.</p></div>
        <input type="checkbox" class="size-5 shrink-0 accent-primary" aria-label="Reduced motion" bind:checked={reducedMotion} onchange={savePreferences}/>
      </CardContent></Card>
    {:else}
      <Card><CardContent class="grid justify-items-start gap-3 p-6 grid-cols-1"><strong>Sign in to manage your account</strong><Button onclick={onSignIn}>Sign in</Button></CardContent></Card>
    {/if}
  </section>
{:else if route.name === 'tournaments'}
  <section class="portal"><div class="eyebrow">EVENTS</div><h1>Tournaments.</h1><div class="bracket"><article><span>SEMIFINAL A</span><strong>Registration pending</strong></article><article><span>SEMIFINAL B</span><strong>Registration pending</strong></article><article><span>FINAL</span><strong>Winner advances here</strong></article></div><p class="page-lede">Event signup and bracket persistence are planned. This page is public.</p></section>
{:else if route.name === 'admin'}
  <section class="portal"><div class="eyebrow">OPERATIONS</div><h1>Admin.</h1>{#if user && admin}<div class="ops-grid"><article><span>CONTROL PLANE</span><strong class:live={cloud?.status === 'ready'}>{cloud?.status?.toUpperCase() ?? 'CHECKING'}</strong><small>{cloud?.provider ?? '--'}</small></article><article><span>QUEUE DEPTH</span><strong>{admin.queueDepth}</strong><small>Players waiting</small></article><article><span>LIVE MATCHES</span><strong>{admin.matches.running ?? 0}</strong><small>{Object.values(admin.viewers).reduce((sum, value) => sum + value, 0)} viewers</small></article><article><span>AGENTS</span><strong>{admin.agents}</strong><small>{admin.matches.finished ?? 0} finished matches</small></article></div>{:else if user}<div class="empty-state"><h2>{loading ? 'Loading service status…' : 'Admin status unavailable'}</h2><p>{pageError || 'This account may not have admin access.'}</p></div>{:else}<div class="auth-gate"><h2>Admin access requires sign-in</h2><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}</section>
{:else if route.name === 'sdk' || route.name === 'api-docs'}
  <DocsPage section={route.name === 'sdk' ? 'sdk' : 'api'} />
{:else}
  <section class="portal narrow-page"><div class="eyebrow">404</div><h1>Route not found.</h1><a class="secondary-action" href="#/">RETURN HOME</a></section>
{/if}
