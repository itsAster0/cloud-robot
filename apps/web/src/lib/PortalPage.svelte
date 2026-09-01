<script lang="ts">
  import type { User } from '@workos-inc/authkit-js';
  import { api, type ListedMatch } from './api';
  import type { AdminStatus, CloudStatus, Match, PlayerStats, QueueStatus, Replay, RobotBox } from './types';
  import type { Route } from './router';

  interface Props { route: Route; user: User | null; match: Match | null; cloud: CloudStatus | null; box?: RobotBox | null; onSignIn: () => void; onCreateMatch: () => void; onReleaseBox: () => Promise<void>; }
  let { route, user, match, cloud, box = null, onSignIn, onCreateMatch, onReleaseBox }: Props = $props();
  let mode = $state('duel'), arenaSize = $state('medium'), queueing = $state(false), filter = $state('all');
  let botPersonality = $state('aggressive'), practiceRegen = $state(false), practiceRamming = $state(false), soloBots = $state(3);
  let soloBotCount = $derived(Number(soloBots));
  let displayName = $state('Robot'), startCommand = $state('lua main.lua');
  let reducedMotion = $state(false), muted = $state(false), notifications = $state(true);
  let customWidth = $state(800), customHeight = $state(500), mapName = $state('Untitled arena');
  let matches = $state<ListedMatch[]>([]), profile = $state<PlayerStats | null>(null), recentMatches = $state<Match[]>([]);
  let leaderboard = $state<{ rank: number; player: PlayerStats; rating: number }[]>([]);
  let queue = $state<QueueStatus>({ status: 'idle' }), replay = $state<Replay | null>(null), admin = $state<AdminStatus | null>(null);
  let loading = $state(false), pageError = $state(''), releasing = $state(false);
  let blockedByActiveRobot = $derived(pageError.includes('box already has an active robot'));
  const luaExample = `local arena = require "arena"

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(observation)
    local enemy = arena.nearest_enemy(observation)
    if enemy then return arena.approach(observation, enemy, 8) end
    return arena.action({ turn = 12 })
  end,
})`;

  function savePreferences() {
    localStorage.setItem('arena-reduced-motion', String(reducedMotion));
    localStorage.setItem('arena-muted', String(muted));
    localStorage.setItem('arena-notifications', String(notifications));
  }
  function requireUser(action: () => void) { if (user) action(); else onSignIn(); }
  let title = $derived(route.name === 'profile' ? `${route.parameter ?? 'Player'} profile` : route.name.replace('-', ' '));
  let visibleMatches = $derived(matches.filter((entry) => filter === 'all' || entry.status === (filter === 'live' ? 'running' : filter)));

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
    if (queue.status === 'matched' && matchId) window.location.hash = `/match/${matchId}`;
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

<svelte:head><title>{title} // Robot Arena</title></svelte:head>
{#if pageError}<div class="portal-alert" role="alert">{pageError}{#if blockedByActiveRobot}<button onclick={releaseActiveMatch} disabled={releasing}>{releasing ? 'RELEASING…' : 'EXIT ACTIVE MATCH'}</button>{/if}</div>{/if}

{#if route.name === 'home'}
  <section class="portal hero-page">
    <div class="hero-copy"><div class="eyebrow">PROGRAMMABLE ROBOT COMBAT</div><h1>Write tactics.<br /><em>Watch them fight.</em></h1><p>Deploy Lua into a persistent SSH box. Server runs deterministic matches. Anyone can watch.</p><div class="hero-actions"><button class="deploy" onclick={() => requireUser(onCreateMatch)}>{user ? 'PLAY NOW' : 'SIGN IN TO PLAY'} <span>↗</span></button><a class="secondary-action" href="#/spectate">WATCH LIVE</a></div></div>
    <article class="feature-card"><div class="eyebrow">{match?.status === 'running' ? 'LIVE NOW' : 'FEATURED MATCH'}</div>{#if match}<h2>{match.mode} // {match.mapId ?? 'Open Field'}</h2><p>{match.robots.map((robot) => robot.displayName).join(' vs ') || 'Lobby forming'}</p><a href={`#/match/${match.matchId}`}>OPEN MATCH</a>{:else}<h2>No match live</h2><p>Create one or open Spectate to browse active matches.</p><a href="#/play">PRACTICE VS BOTS</a>{/if}</article>
    <section class="quick-grid" aria-label="How to start"><article><b>01</b><h3>Provision box</h3><p>One persistent, resource-limited workspace per account.</p></article><article><b>02</b><h3>Deploy Lua</h3><p>Edit <code>/workspace/main.lua</code> over SSH.</p></article><article><b>03</b><h3>Enter arena</h3><p>Agent sends intent. Server owns state and scoring.</p></article></section>
  </section>
{:else if route.name === 'play'}
  <section class="portal narrow-page"><div class="eyebrow">MATCHMAKING</div><h1>Enter arena.</h1><p class="page-lede">Join the duel queue, field a 5v5 squad against ten bots, or go solo in a free-for-all sandbox. Queue fills with a bot when another player does not arrive.</p>
    {#if box?.activeMatchId}
      <div class="box-match-note" data-tone="ok"><span>IN MATCH</span><div>Your box is committed to <a href={`#/match/${box.activeMatchId}`}>match {box.activeMatchId.slice(0, 8)}</a>. Queueing stays blocked until it ends. <a href={`#/match/${box.activeMatchId}`}>RESUME MATCH →</a> <button onclick={releaseActiveMatch} disabled={releasing}>{releasing ? 'RELEASING…' : 'EXIT MATCH'}</button></div></div>
    {/if}
    <div class="form-grid"><label>Mode<select bind:value={mode} disabled={queueing}><option value="duel">Duel queue</option><option value="squad">Squad 5v5 vs bots</option><option value="solo">Solo free-for-all</option></select></label>{#if mode !== 'duel'}<label>Arena size<select bind:value={arenaSize} disabled={queueing || mode === 'squad'}><option value="small">Small · 600 × 400</option><option value="medium">Medium · 800 × 500</option><option value="large">Large · 1200 × 800</option></select></label>{/if}{#if mode === 'squad'}<label>Squad roster<input value="10 bots · 5 per side" disabled /></label>{/if}{#if mode === 'solo'}<label>Bot opponents<select bind:value={soloBots} disabled={queueing}>{#each Array(8) as _, count}<option value={count}>{count === 0 ? 'No bots · sandbox' : `${count} bot${count > 1 ? 's' : ''}`}</option>{/each}</select></label>{/if}{#if mode !== 'duel'}<label>Bot personality<select bind:value={botPersonality} disabled={queueing}><option value="aggressive">Aggressive</option><option value="evasive">Evasive</option><option value="camper">Camper</option></select></label><label>Regen after 5 s calm<input type="checkbox" bind:checked={practiceRegen} disabled={queueing} /></label><label>Ramming damage<input type="checkbox" bind:checked={practiceRamming} disabled={queueing} /></label>{/if}<label>Robot name<input bind:value={displayName} maxlength="32" disabled={queueing} /></label><label>Start command<input bind:value={startCommand} maxlength="256" disabled={queueing} /></label></div>
    {#if mode === 'squad'}<p class="page-lede">The lobby opens with five bots per side. Registering on a team replaces one of its bots; both teams keep five robots.</p>{/if}
    {#if user}<button class="deploy" onclick={mode === 'duel' ? toggleQueue : startLobby}>{queueing ? `LEAVE QUEUE · ${queue.status.toUpperCase()}` : mode === 'squad' ? 'CREATE SQUAD LOBBY' : mode === 'solo' ? 'CREATE SOLO ARENA' : 'JOIN DUEL QUEUE'} <span>↗</span></button>{:else}<div class="auth-gate"><h2>Sign in to play</h2><p>Watching, profiles, results, and docs stay public.</p><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}
    <div class="roster-preview">{#if mode === 'squad'}<div><span>RED // 5</span><strong>{displayName || user?.firstName || 'You'} + 4 bots</strong></div><div><span>BLUE // 5</span><strong>5 bots</strong></div>{:else if mode === 'solo'}<div><span>FREE-FOR-ALL</span><strong>{displayName || user?.firstName || 'You'}</strong></div><div><span>OPPONENTS</span><strong>{soloBotCount === 0 ? 'Empty sandbox' : `${soloBotCount} bot${soloBotCount > 1 ? 's' : ''}`}</strong></div>{:else}<div><span>RED // R</span><strong>{displayName || user?.firstName || 'You'}</strong></div><div><span>BLUE // B</span><strong>{queue.status === 'waiting' ? 'Searching…' : 'Waiting'}</strong></div>{/if}</div>
  </section>
{:else if route.name === 'matches' || route.name === 'spectate'}
  <section class="portal"><div class="page-head"><div><div class="eyebrow">{route.name === 'spectate' ? 'PUBLIC LIVE VIEW' : 'MATCH ARCHIVE'}</div><h1>{route.name === 'spectate' ? 'Spectate.' : 'Match history.'}</h1></div><label class="filter">Filter<select bind:value={filter}><option value="all">All modes</option><option value="live">Live</option><option value="finished">Finished</option></select></label></div>
    <div class="card-list">{#each visibleMatches as listed (listed.matchId)}<a class="match-card" href={listed.status === 'finished' ? `#/match/${listed.matchId}/detail` : `#/match/${listed.matchId}`}><div><span class="pill" data-tone={listed.status === 'running' ? 'ok' : 'idle'}>{listed.status}</span><h2>{listed.robots.map((robot) => robot.displayName).join(' vs ') || 'Open lobby'}</h2><p>{listed.mode} · {listed.mapId ?? 'open-field'} · {new Date(listed.createdAt).toLocaleString()}{#if listed.status === 'running'} · {listed.viewers ?? 0} watching{/if}</p></div><strong>{listed.status === 'finished' ? `${listed.winnerTeam?.toUpperCase() ?? 'DRAW'} ›` : 'WATCH ›'}</strong></a>{:else}<div class="empty-state"><h2>{loading ? 'Loading matches…' : 'No matching matches'}</h2><p>{route.name === 'spectate' ? 'No matches are live now.' : 'Finished and active matches will appear here.'}</p></div>{/each}</div>
  </section>
{:else if route.name === 'match-detail'}
  <section class="portal narrow-page"><div class="eyebrow">MATCH RECAP</div><h1>{match?.winnerTeam ? `${match.winnerTeam.toUpperCase()} wins.` : 'Result unavailable.'}</h1><div class="recap-grid"><div><span>DURATION</span><strong>{match?.startedAt && match?.finishedAt ? `${Math.round((Date.parse(match.finishedAt) - Date.parse(match.startedAt)) / 1000)} s` : '--'}</strong></div><div><span>MAP</span><strong>{match?.mapId ?? 'open-field'}</strong></div><div><span>MODE</span><strong>{match?.mode ?? '--'}</strong></div><div><span>REPLAY</span><strong>{replay?.complete ? 'COMPLETE' : `${replay?.events.length ?? 0} SUMMARY EVENTS`}</strong></div></div>{#each match?.robotSummaries ?? [] as robot}<article class="stat-row"><b class={robot.team === 'red' || robot.team === 'blue' ? robot.team : 'solo'}>{robot.team === 'red' ? 'R' : robot.team === 'blue' ? 'B' : 'S'}</b><strong>{robot.name}</strong><span>{robot.kills ?? 0} kills</span><span>{robot.damageDealt ?? 0} dealt</span><span>{robot.itemsPickedUp ?? 0} items</span></article>{/each}{#if replay?.events.length}<div class="replay-feed">{#each replay.events as event}<div><time>T{event.tick}</time><strong>{event.type.replace(/_/g, ' ')}</strong><span>{event.message ?? (event.damage ? `${event.damage} damage` : event.robotId)}</span></div>{/each}</div>{/if}<a class="secondary-action" href={`#/match/${route.parameter}`}>OPEN ARENA VIEW</a></section>
{:else if route.name === 'profile'}
  <section class="portal narrow-page"><div class="eyebrow">PLAYER PROFILE</div><h1>{profile?.handle ?? route.parameter}</h1>{#if profile}<div class="profile-card"><div class="profile-mark">{profile.handle.slice(0, 2).toUpperCase()}</div><div><span>PLAYER ID</span><strong>{profile.playerId.slice(0, 12)}</strong><p>Stats updated {new Date(profile.updatedAt).toLocaleString()}.</p></div></div><div class="recap-grid"><div><span>DUEL RATING</span><strong>{profile.ratings.duel ?? 1000}</strong></div><div><span>WINS</span><strong>{profile.wins}</strong></div><div><span>LOSSES / DRAWS</span><strong>{profile.losses} / {profile.draws}</strong></div><div><span>DAMAGE DEALT</span><strong>{profile.damageDealt}</strong></div></div>{#each recentMatches as recent}<a class="match-card" href={`#/match/${recent.matchId}/detail`}><div><strong>{recent.mode} · {recent.mapId}</strong><p>{new Date(recent.createdAt).toLocaleString()}</p></div><strong>{recent.winnerTeam?.toUpperCase() ?? recent.status.toUpperCase()} ›</strong></a>{/each}{:else if !loading}<div class="empty-state"><h2>Profile not found</h2><p>No recorded stats for this handle.</p></div>{/if}</section>
{:else if route.name === 'leaderboard'}
  <section class="portal"><div class="eyebrow">RANKED // DUEL</div><h1>Leaderboard.</h1><div class="ladder"><div class="ladder-head"><span>RANK</span><span>PLAYER</span><span>RATING</span><span>W/L</span></div>{#each leaderboard as entry}<a class="ladder-row" href={`#/profile/${encodeURIComponent(entry.player.handle)}`}><span>#{entry.rank}</span><strong>{entry.player.handle}</strong><span>{entry.rating}</span><span>{entry.player.wins}/{entry.player.losses}</span></a>{:else}<div class="empty-state"><h2>{loading ? 'Loading ladder…' : 'No ranked results'}</h2><p>Completed duel results will fill this ladder.</p></div>{/each}</div></section>
{:else if route.name === 'create'}
  <section class="portal"><div class="eyebrow">WORKSHOP</div><h1>Create arena.</h1>{#if user}<div class="workshop-grid"><article class="console-card"><label>Map name<input bind:value={mapName} maxlength="48" /></label><div class="form-grid"><label>Width<input type="number" min="400" max="1600" bind:value={customWidth} /></label><label>Height<input type="number" min="300" max="1000" bind:value={customHeight} /></label></div><button class="secondary-action" disabled>EXPORT JSON · COMING LATER</button></article><article class="map-preview" style={`aspect-ratio: ${customWidth}/${customHeight}`}><span>{mapName}</span><i>CUSTOM {customWidth} × {customHeight}</i></article></div>{:else}<div class="auth-gate"><h2>Sign in to use Workshop</h2><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}</section>
{:else if route.name === 'settings'}
  <section class="portal narrow-page"><div class="eyebrow">ACCOUNT</div><h1>Settings.</h1>{#if user}<div class="settings-list"><label><span>Reduced motion<small>Disable arena interpolation and motion effects.</small></span><input type="checkbox" bind:checked={reducedMotion} /></label><label><span>Master mute<small>Mute match sounds when audio effects land.</small></span><input type="checkbox" bind:checked={muted} /></label><label><span>Notifications<small>Allow queue and match-ready notices.</small></span><input type="checkbox" bind:checked={notifications} /></label></div><button class="deploy" onclick={savePreferences}>SAVE PREFERENCES</button><a class="secondary-action" href="#/box">MANAGE SSH KEY IN MY BOX</a>{:else}<div class="auth-gate"><h2>Sign in to edit settings</h2><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}</section>
{:else if route.name === 'tournaments'}
  <section class="portal"><div class="eyebrow">EVENTS</div><h1>Tournaments.</h1><div class="bracket"><article><span>SEMIFINAL A</span><strong>Registration pending</strong></article><article><span>SEMIFINAL B</span><strong>Registration pending</strong></article><article><span>FINAL</span><strong>Winner advances here</strong></article></div><p class="page-lede">Event signup and bracket persistence are planned. This page is public.</p></section>
{:else if route.name === 'admin'}
  <section class="portal"><div class="eyebrow">OPERATIONS</div><h1>Admin.</h1>{#if user && admin}<div class="ops-grid"><article><span>CONTROL PLANE</span><strong class:live={cloud?.status === 'ready'}>{cloud?.status?.toUpperCase() ?? 'CHECKING'}</strong><small>{cloud?.provider ?? '--'}</small></article><article><span>QUEUE DEPTH</span><strong>{admin.queueDepth}</strong><small>Players waiting</small></article><article><span>LIVE MATCHES</span><strong>{admin.matches.running ?? 0}</strong><small>{Object.values(admin.viewers).reduce((sum, value) => sum + value, 0)} viewers</small></article><article><span>AGENTS</span><strong>{admin.agents}</strong><small>{admin.matches.finished ?? 0} finished matches</small></article></div>{:else if user}<div class="empty-state"><h2>{loading ? 'Loading service status…' : 'Admin status unavailable'}</h2><p>{pageError || 'This account may not have admin access.'}</p></div>{:else}<div class="auth-gate"><h2>Admin access requires sign-in</h2><button class="deploy" onclick={onSignIn}>SIGN IN</button></div>{/if}</section>
{:else if route.name === 'sdk' || route.name === 'api-docs'}
  <section class="docs-page"><aside><div class="eyebrow">{route.name === 'sdk' ? 'LUA SDK // v0.2.0' : 'HTTP + WS API'}</div><h1>Build robot.<br /><em>Own runtime.</em></h1><p>Public viewer reads need no session. Resource ownership and mutations require sign-in.</p><a href={route.name === 'sdk' ? '#/docs/sdk' : '#/docs/api'}>Quickstart</a><a href="#/docs/sdk">Lua SDK</a><a href="#/docs/api">API schema</a></aside><article>{#if route.name === 'sdk'}<section><div class="eyebrow">QUICKSTART</div><h2>Continuous agent loop</h2><p>Save as <code>main.lua</code>. Start with <code>lua main.lua</code>.</p><pre><code>{luaExample}</code></pre></section><section><div class="eyebrow">OBSERVATION</div><h2>World state</h2><p>Each observation includes tick, self, visible robots, projectiles, items, obstacles, arena bounds, and recent events when supported by server protocol.</p><pre><code>{`observation = {
  tick, self, robots, projectiles,
  items = { { itemId, type, x, y, active, respawnTick } },
  obstacles = { { id, shape, x, y, radius, width, height } },
  arena = { width, height, map_id }
}`}</code></pre></section><section><div class="eyebrow">HELPERS</div><div class="function-grid"><div><code>arena.action(options)</code><p>Return movement, turn, aim, fire, logs, and equipment intent.</p></div><div><code>arena.nearest_enemy(obs)</code><p>Find nearest live opponent.</p></div><div><code>arena.approach(obs, target, speed)</code><p>Track and chase target.</p></div><div><code>arena.strafe(obs, target, direction)</code><p>Circle target while firing.</p></div></div></section>{:else}<section><div class="eyebrow">HTTP</div><h2>Match resources</h2><pre><code>{`GET    /api/matches                public
POST   /api/matches                 session required
GET    /api/matches/{id}            public
GET    /api/matches/{id}/replay     public
GET    /api/profiles/{handle}       public
GET    /api/leaderboard             public
GET    /api/queue                   session required
POST   /api/queue                   session required
DELETE /api/queue                   session required
POST   /api/matches/{id}/robots     session required
DELETE /api/matches/{id}/robots     session required
POST   /api/matches/{id}/start      session required`}</code></pre></section><section><div class="eyebrow">VIEWER WEBSOCKET</div><h2>Anonymous snapshots</h2><pre><code>{`GET /ws/matches/{id}

snapshot: { type, version, matchId, sequence, tick,
  status, winnerTeam, robots, projectiles, items,
  obstacles, events, width, height, mapId }`}</code></pre></section>{/if}</article></section>
{:else}
  <section class="portal narrow-page"><div class="eyebrow">404</div><h1>Route not found.</h1><a class="secondary-action" href="#/">RETURN HOME</a></section>
{/if}
