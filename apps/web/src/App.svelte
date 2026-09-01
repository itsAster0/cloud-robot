<script lang="ts">
  import { onMount } from 'svelte';
  import type { User } from '@workos-inc/authkit-js';
  import Arena from './lib/Arena.svelte';
  import BoxConsole from './lib/BoxConsole.svelte';
  import { api, setTokenProvider } from './lib/api';
  import { accessToken, authConfigured, clearRedirectCallback, initializeAuth, redirectCallbackPending, signIn, signOut } from './lib/auth';
  import { isNewerSnapshot, type AgentEnrollment, type ArenaEvent, type CloudStatus, type Match, type RobotBox, type Snapshot, type Team } from './lib/types';

  // Hash routes keep back/forward working without a router dependency.
  type Route = 'arena' | 'box' | 'docs';
  const ROUTES: readonly Route[] = ['arena', 'box', 'docs'];
  function routeFromLocation(): Route {
    const candidate = window.location.hash.replace(/^#\/?/, '');
    return (ROUTES as readonly string[]).includes(candidate) ? (candidate as Route) : 'arena';
  }
  function go(route: Route) {
    if (routeFromLocation() === route) return;
    window.location.hash = `/${route}`;
  }

  const luaExample = `local arena = require "arena"

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(observation)
    local enemy = arena.nearest_enemy(observation)
    if enemy then
      return arena.approach(observation, enemy, 8)
    end
    return arena.action({ turn = 12 })
  end,
})`;

  let view = $state<Route>(routeFromLocation());
  let team = $state<Team>('red');
  let displayName = $state('Ada');
  let startCommand = $state('lua main.lua');
  let match = $state<Match | null>(null);
  let enrollment = $state<AgentEnrollment | null>(null);
  let snapshot = $state<Snapshot | null>(null);
  let cloud = $state<CloudStatus | null>(null);
  let user = $state<User | null>(null);
  let signingIn = $state(redirectCallbackPending());
  let robotBox = $state<RobotBox | null>(null);
  let sshKey = $state('');
  let mainSource = $state<string | null>(null);
  let joinCode = $state('');
  let busy = $state(false);
  let pending = $state<Record<string, boolean>>({});
  let copied = $state<'ssh' | 'link' | null>(null);
  let error = $state('');
  let socketState = $state<'offline' | 'connecting' | 'live'>('offline');
  let connectedAgents = $state<Record<string, boolean>>({});
  let feed = $state<FeedEntry[]>([]);
  let socket: WebSocket | null = null;
  let boxPoll: ReturnType<typeof setInterval> | null = null;

  interface FeedEntry {
    id: string;
    tone: 'ok' | 'bad';
    label: string;
    text: string;
  }

  let hasRed = $derived(match?.robots.some((robot) => robot.team === 'red') ?? false);
  let hasBlue = $derived(match?.robots.some((robot) => robot.team === 'blue') ?? false);
  let allConnected = $derived(match?.robots.every((robot) => connectedAgents[robot.robotId]) ?? false);
  let canStart = $derived(match?.status === 'lobby' && hasRed && hasBlue && allConnected);
  let myRobot = $derived(match?.robots.find((robot) => robot.ownerBoxId === robotBox?.boxId) ?? null);
  let latestFeed = $derived(feed.slice(-14).reverse());

  function robotName(robotId?: string) {
    return match?.robots.find((robot) => robot.robotId === robotId)?.displayName ?? robotId?.slice(0, 6) ?? '?';
  }

  function feedEntry(seq: number, index: number, event: ArenaEvent): FeedEntry {
    const label = event.type.replace(/_/g, ' ').toUpperCase();
    const fatal = event.type === 'robot_destroyed' || event.type === 'robot_failed';
    let text = event.message ?? '';
    if (event.type === 'shot_fired') text = `${robotName(event.robotId)} fired`;
    else if (event.type === 'hit') text = `${robotName(event.robotId)} hit ${robotName(event.targetId)} for ${event.damage} dmg`;
    else if (event.type === 'robot_destroyed') text = `${robotName(event.robotId)} destroyed by ${robotName(event.targetId)}`;
    else if (event.type === 'robot_failed') text = `${robotName(event.robotId)} script failed`;
    return { id: `${seq}:${index}:${label}`, tone: fatal ? 'bad' : 'ok', label, text };
  }

  onMount(() => {
    const onHash = () => { view = routeFromLocation(); };
    window.addEventListener('hashchange', onHash);
    void initialize();
    return () => {
      window.removeEventListener('hashchange', onHash);
      socket?.close();
      if (boxPoll) clearInterval(boxPoll);
    };
  });

  async function initialize() {
    // authkit-js swallows a failed code exchange internally (console only), so
    // detect the callback ourselves before initializeAuth cleans the URL.
    const pendingCallback = redirectCallbackPending();
    try {
      user = await initializeAuth();
      setTokenProvider(accessToken);
      if (user?.firstName) displayName = user.firstName;
      if (user) {
        robotBox = await api.ensureBox();
        boxPoll = setInterval(() => void refreshBox(), 5000);
        void loadMain(true);
      }
    } catch (failure) {
      setError(failure);
      // A consumed or abandoned ?code= retry-loop breaks every reload until removed.
      if (redirectCallbackPending()) clearRedirectCallback();
    }
    if (pendingCallback && !user) {
      setError(`Sign-in callback failed: WorkOS rejected the token exchange. Confirm ${window.location.origin} is listed under WorkOS Dashboard → Authentication → Sessions → CORS, then try again.`);
    }
    signingIn = redirectCallbackPending();
    try { cloud = await api.cloudStatus(); } catch (failure) { setError(failure); }
    const matchId = new URL(window.location.href).searchParams.get('match');
    if (matchId) {
      try {
        match = await api.getMatch(matchId);
        connect(matchId);
        // Joining a shared match should suggest whichever side is still open.
        if (match.robots.some((robot) => robot.team === 'red') && !match.robots.some((robot) => robot.team === 'blue')) team = 'blue';
        else if (match.robots.some((robot) => robot.team === 'blue') && !match.robots.some((robot) => robot.team === 'red')) team = 'red';
      } catch {
        // Shared invite links outlive matches (stack restarts, fresh Floci).
        // Drop the stale param so the session starts clean instead of a FAULT.
        const url = new URL(window.location.href);
        url.searchParams.delete('match');
        history.replaceState({}, '', url.toString());
        setError('This match link is no longer valid — the match may have ended or the stack restarted. Create a new match to continue.');
      }
    }
  }

  async function handleSignIn() {
    signingIn = true;
    try {
      await signIn();
    } catch (failure) {
      setError(failure);
    } finally {
      signingIn = false;
    }
  }

  function setError(failure: unknown) { error = failure instanceof Error ? failure.message : String(failure); }

  // Per-action loading state so several controls can run without freezing
  // unrelated buttons; `busy` still gates simultaneous match mutations.
  async function withPending(key: string, action: () => Promise<void>) {
    if (pending[key] || busy) return;
    pending = { ...pending, [key]: true };
    try { await action(); } catch (failure) { setError(failure); } finally { pending = { ...pending, [key]: false }; }
  }

  function working(key: string, fallback = 'WORKING…') {
    return pending[key] ? fallback : null;
  }

  async function copyFeedback(key: 'ssh' | 'link', text: string) {
    await navigator.clipboard.writeText(text);
    copied = key;
    setTimeout(() => { if (copied === key) copied = null; }, 1600);
  }

  const createMatch = () => withPending('create', async () => {
    busy = true; error = '';
    try {
      match = await api.createMatch();
      const url = new URL(window.location.href);
      url.searchParams.set('match', match.matchId);
      url.hash = '/arena';
      history.replaceState({}, '', url);
      connect(match.matchId);
    } finally { busy = false; }
  });

  const registerRobot = () => withPending('register', async () => {
    if (!match) return;
    busy = true; error = '';
    try {
      const response = await api.submitRobot(match.matchId, { displayName, team, startCommand, runtime: 'lua5.4' });
      match = response.match;
      enrollment = response.agent;
      await refreshBox();
    } finally { busy = false; }
  });

  const startMatch = () => withPending('start', async () => {
    if (!match) return;
    busy = true; error = '';
    try { match = await api.startMatch(match.matchId); } finally { busy = false; }
  });

  const withdrawRobot = () => withPending('withdraw', async () => {
    if (!match) return;
    error = '';
    match = await api.withdrawRobot(match.matchId);
    enrollment = null;
    await refreshBox();
  });

  // Accepts a bare match ID or a pasted invite link; joining just loads the
  // lobby and opens the watcher socket, registration stays a separate step.
  const joinMatch = () => withPending('join', async () => {
    error = '';
    const raw = joinCode.trim();
    if (!raw) return;
    const code = raw.includes('match=') ? (new URL(raw).searchParams.get('match') ?? raw) : raw;
    const found = await api.getMatch(code);
    joinCode = '';
    match = found;
    const url = new URL(window.location.href);
    url.searchParams.set('match', found.matchId);
    url.hash = '/arena';
    history.replaceState({}, '', url);
    connect(found.matchId);
  });

  const refreshBox = () => withPending('refresh', async () => {
    if (!user) return;
    robotBox = await api.getBox();
  });

  // Silent failures keep the viewer quiet when the box is still booting.
  async function loadMain(silent = false) {
    if (pending.main || !user) return;
    pending = { ...pending, main: true };
    try {
      mainSource = (await api.getBoxMain()).source;
    } catch (failure) {
      if (!silent) setError(failure);
    } finally {
      pending = { ...pending, main: false };
    }
  }

  const saveSSHKey = () => withPending('key', async () => {
    error = '';
    robotBox = await api.setBoxKey(sshKey);
    sshKey = '';
  });

  const restartBox = () => withPending('restart', async () => {
    error = '';
    robotBox = await api.restartBox();
  });

  const provisionBox = () => withPending('provision', async () => {
    error = '';
    robotBox = await api.ensureBox();
  });

  const copySSH = () => {
    if (!robotBox) return;
    return copyFeedback('ssh', `ssh -p ${robotBox.sshPort} ${robotBox.sshUser}@${robotBox.sshHost}`);
  };

  // Invite links always land on the arena view even if copied from another page.
  const copyInvite = () => {
    const url = new URL(window.location.href);
    if (match) url.searchParams.set('match', match.matchId);
    url.hash = '/arena';
    return copyFeedback('link', url.toString());
  };

  function jump(id: string) {
    return (click: MouseEvent) => {
      click.preventDefault();
      document.getElementById(id)?.scrollIntoView({ behavior: 'smooth' });
    };
  }

  function bytes(value = 0) { return `${(value / (1024 * 1024)).toFixed(1)} MB`; }

  function connect(matchId: string) {
    socket?.close();
    socketState = 'connecting';
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${scheme}//${window.location.host}/ws/matches/${encodeURIComponent(matchId)}`);
    socket.onopen = () => { socketState = 'live'; };
    socket.onclose = () => { socketState = 'offline'; };
    socket.onerror = () => { socketState = 'offline'; };
    socket.onmessage = ({ data }) => {
      const event = JSON.parse(data);
      if (event.type === 'snapshot' && isNewerSnapshot(snapshot, event)) {
        snapshot = event;
        const incoming = (event.events ?? []).map((entry: ArenaEvent, index: number) => feedEntry(event.sequence, index, entry));
        if (incoming.length) feed = [...feed, ...incoming].slice(-40);
      }
      if (event.type === 'agent_status') connectedAgents = { ...connectedAgents, [event.robotId]: event.connected };
      if (event.type === 'agent_status_snapshot') connectedAgents = event.agents;
      if (event.match) match = event.match;
      if (event.type === 'error') error = event.error;
    };
  }
</script>

<svelte:head><title>Robot Arena // Control Room</title></svelte:head>

<main class="shell">
  <header class="topbar">
    <button class="brand" onclick={() => go('arena')} aria-label="Go to arena"><span class="brand-mark" aria-hidden="true">RA</span><div><strong>ROBOT ARENA</strong><small>CONTINUOUS AGENT COMBAT</small></div></button>
    <nav class="main-nav" aria-label="Primary">
      <button class:active={view === 'arena'} aria-current={view === 'arena' ? 'page' : undefined} onclick={() => go('arena')}><i class="nav-dot" class:ok={socketState === 'live'} aria-hidden="true"></i>ARENA</button>
      <button class:active={view === 'box'} aria-current={view === 'box' ? 'page' : undefined} onclick={() => go('box')}><i class="nav-dot" class:ok={robotBox?.status === 'running'} class:bad={robotBox?.status === 'failed' || !!robotBox?.error} aria-hidden="true"></i>MY BOX</button>
      <button class:active={view === 'docs'} aria-current={view === 'docs' ? 'page' : undefined} onclick={() => go('docs')}>SDK DOCS</button>
    </nav>
    <div class="account">
      <div class="system-state" class:bad={cloud?.status !== 'ready'}><span></span>{cloud?.status === 'ready' ? 'FLOCI READY' : 'CLOUD CHECK'}</div>
      {#if user}<button class="auth-button" onclick={signOut}>{user.firstName ?? user.email} · SIGN OUT</button>{:else}<button class="auth-button" disabled={signingIn} onclick={handleSignIn}>{signingIn ? 'SIGNING IN…' : 'SIGN IN'}</button>{/if}
    </div>
  </header>

  {#if error}<div class="error-banner" role="alert"><span>FAULT</span>{error}<button onclick={() => (error = '')}>DISMISS</button></div>{/if}

  {#if view === 'docs'}
    <section class="docs-page">
      <aside><div class="eyebrow">LUA SDK // v0.1</div><h1>Build robot.<br /><em>Own runtime.</em></h1><p>Code stays inside assigned box. SDK keeps one outbound WebSocket alive and answers live observations.</p><a href="#/docs" onclick={jump('quickstart')}>01 Quickstart</a><a href="#/docs" onclick={jump('functions')}>02 Functions</a><a href="#/docs" onclick={jump('protocol')}>03 Protocol</a><a href="#/docs" onclick={jump('limits')}>04 Limits</a></aside>
      <article>
        <section id="quickstart"><div class="eyebrow">01 // QUICKSTART</div><h2>Continuous agent loop</h2><p>Save as <code>main.lua</code>. Start command: <code>lua main.lua</code>.</p><pre><code>{luaExample}</code></pre></section>
        <section id="functions"><div class="eyebrow">02 // FUNCTIONS</div><h2>Decision helpers</h2><div class="function-grid"><div><code>arena.action(options)</code><p>Move, turn, fire, log, report equipment.</p></div><div><code>arena.nearest_enemy(obs)</code><p>Nearest live opponent and distance.</p></div><div><code>arena.approach(obs, target, speed)</code><p>Track, chase, and fire.</p></div><div><code>arena.strafe(obs, target, direction)</code><p>Circle opponent while firing.</p></div><div><code>arena.distance(a, b)</code><p>Euclidean distance between entities.</p></div><div><code>arena.bearing(a, b)</code><p>Target heading in degrees.</p></div></div></section>
        <section id="protocol"><div class="eyebrow">03 // PROTOCOL</div><h2>Persistent WebSocket</h2><p>Server sends 10 observations each second. Agent returns latest movement, aim, fire state, logs, equipment, memory, and compute time. UI measures end-to-end response independently.</p></section>
        <section id="limits"><div class="eyebrow">04 // LIMITS</div><h2>Fair play envelope</h2><ul><li>150 ms response deadline</li><li>8 movement units per update</li><li>18 degrees rotation per update</li><li>One credential per robot box</li><li>WSS required outside local development</li></ul></section>
      </article>
    </section>
  {:else if view === 'box'}
    <BoxConsole
      {user}
      box={robotBox}
      bind:sshKey
      {pending}
      copiedSsh={copied === 'ssh'}
      authConfigured={authConfigured()}
      {mainSource}
      onProvision={provisionBox}
      onSaveKey={saveSSHKey}
      onRestart={restartBox}
      onCopySsh={copySSH}
      onLoadMain={() => void loadMain()}
    />
  {:else}
    <section class="workspace">
      <aside class="mission-panel">
        <div class="eyebrow">MATCH // {match?.status?.toUpperCase() ?? 'NEW'}</div>
        <h1>Ship code.<br /><em>Keep it alive.</em></h1>
        <p class="lede">SSH into assigned box, deploy anything behind Lua SDK, and keep agent WebSocket running.</p>

        {#if !user}
          <div class="box-card"><div class="eyebrow">SSH BOX</div><strong>SIGN IN REQUIRED</strong><p>{authConfigured() ? 'Sign in to link this box to your account and receive a persistent workspace.' : 'WorkOS client ID is not configured; set VITE_WORKOS_CLIENT_ID and rebuild the web app.'}</p></div>
        {:else if robotBox}
          <div class="box-strip">
            <div class="strip-line"><span class="eyebrow">YOUR BOX</span><strong>{robotBox.status.toUpperCase()} · {robotBox.agentStatus.toUpperCase()}</strong></div>
            <code>ssh -p {robotBox.sshPort} {robotBox.sshUser}@{robotBox.sshHost}</code>
            <div class="quota"><span>{bytes(robotBox.usageBytes)} / {bytes(robotBox.limits.storageBytes)}</span><button onclick={() => go('box')}>MANAGE BOX →</button></div>
          </div>
        {:else}
          <div class="box-card"><div class="eyebrow">SSH BOX</div><strong>NOT PROVISIONED</strong><p>Open the box console to provision a persistent container for your account.</p><button class="save-key" onclick={provisionBox} disabled={busy || pending.provision}>{pending.provision ? 'PROVISIONING…' : 'PROVISION BOX'}</button></div>
        {/if}

        {#if !match}
          <label>Match code<input bind:value={joinCode} maxlength="300" placeholder="Paste match ID or invite link" /></label>
          <button class="join-match" onclick={joinMatch} disabled={busy || pending.join || !user || !joinCode.trim()}>{working('join', 'JOINING…') ?? 'JOIN MATCH BY CODE'}</button>
        {/if}

        {#if match}<div class="match-id"><span>MATCH ID</span><code>{match.matchId.slice(0, 13)}</code><button onclick={copyInvite} disabled={pending.link}>{copied === 'link' ? 'COPIED ✓' : 'COPY LINK'}</button></div>{/if}
        <label>Robot name<input bind:value={displayName} maxlength="32" disabled={match?.status !== 'lobby' && !!match} /></label>
        <fieldset disabled={match?.status !== 'lobby' && !!match}><legend>Team</legend><div class="team-switch"><button class:active={team === 'red'} onclick={() => (team = 'red')}>RED</button><button class:active={team === 'blue'} onclick={() => (team = 'blue')}>BLUE</button></div></fieldset>
        <label>Start command<input bind:value={startCommand} maxlength="256" disabled={match?.status !== 'lobby' && !!match} /></label>

        <div class="facts"><div><span>MODE</span><strong>REAL-TIME 1V1</strong></div><div><span>TICK</span><strong>{snapshot?.tick ?? 0}</strong></div><div><span>VIEW STREAM</span><strong class:live={socketState === 'live'}>{socketState.toUpperCase()}</strong></div></div>

        {#if !match}<button class="deploy" onclick={createMatch} disabled={busy || pending.create || !user || !robotBox?.keyFingerprint}>{working('create', 'CREATING…') ?? 'CREATE MATCH'} <span>↗</span></button>
        {:else if match.status === 'lobby'}<div class="lobby-actions">
            <button class="deploy" onclick={registerRobot} disabled={busy || pending.register || !user || !robotBox?.keyFingerprint || !!myRobot}>{myRobot ? `REGISTERED AS ${myRobot.team.toUpperCase()}` : (working('register', 'REGISTERING…') ?? 'REGISTER BOX')} <span>↗</span></button>
            <button class="start-match" onclick={startMatch} disabled={busy || pending.start || !canStart}>{working('start', 'STARTING…') ?? 'START NOW (AUTO WHEN READY)'}</button>
            {#if myRobot}<button class="withdraw" onclick={withdrawRobot} disabled={busy || pending.withdraw}>{working('withdraw', 'WITHDRAWING…') ?? 'WITHDRAW ROBOT'}</button>{/if}
          </div>
        {:else}<div class="match-result" class:finished={match.status === 'finished'}><span>{match.status === 'finished' ? 'WINNER' : 'MATCH STATE'}</span><strong>{match.winnerTeam?.toUpperCase() ?? match.status.toUpperCase()}</strong></div>{/if}
      </aside>

      <section class="arena-panel">
        <div class="panel-head"><div><span class="status-dot"></span>ARENA // LIVE</div><span>{snapshot?.status.toUpperCase() ?? 'WAITING FOR AGENTS'}</span></div>
        <div class="arena-stage"><Arena {snapshot} /></div>
        <div class="stats-strip">
          {#each snapshot?.robots ?? [] as robot}<div><span>{robot.name}</span><strong>{robot.hp}<small> HP</small></strong><i>{robot.avgResponseMs?.toFixed(1) ?? '0.0'} ms avg</i></div>{/each}
          {#if !snapshot}<div><span>RESPONSE</span><strong>--</strong><i>agent telemetry</i></div><div><span>HEALTH</span><strong>--</strong><i>waiting for match</i></div>{/if}
        </div>
        {#if enrollment}
          <div class="enrollment-card"><div><div class="eyebrow">BOX AGENT // AUTOMATIC</div><h3>{enrollment.status}</h3><p>Server configured box supervisor. Agent reconnects without exposing token to browser.</p></div><button onclick={refreshBox} disabled={pending.refresh}>{working('refresh', 'REFRESHING…') ?? 'REFRESH STATUS'}</button></div>
        {:else}
          <div class="connection-note"><span>01</span><p>Register box</p><span>02</span><p>SSH and deploy</p><span>03</span><p>Agent connects outbound</p><span>04</span><p>Start match</p></div>
        {/if}
      </section>

      <aside class="telemetry-panel">
        <div class="panel-head"><div>ROBOT TELEMETRY</div><span>10 HZ</span></div>
        <div class="roster">
          <div class="eyebrow">ROSTER</div>
          {#if match?.robots.length}{#each match.robots as robot}{@const runtime = snapshot?.robots.find((entry) => entry.robotId === robot.robotId)}<div class="robot-card"><div class="roster-row"><b class={robot.team}>{robot.team.slice(0, 1).toUpperCase()}</b><strong>{robot.displayName}</strong><span class:live={connectedAgents[robot.robotId]}>{connectedAgents[robot.robotId] ? 'CONNECTED' : 'OFFLINE'}</span></div><div class="hp-track" aria-label="hit points"><i class:low={(runtime?.hp ?? 100) <= 30} style="width: {runtime?.hp ?? 100}%"></i></div><div class="metric-grid"><div><span>HP</span><strong>{runtime?.hp ?? 100}</strong></div><div><span>AVG</span><strong>{runtime?.avgResponseMs?.toFixed(1) ?? '--'} ms</strong></div><div><span>COMPUTE</span><strong>{runtime?.computeMs?.toFixed(2) ?? '--'} ms</strong></div><div><span>MEMORY</span><strong>{runtime?.memoryMb?.toFixed(0) ?? '--'} MB</strong></div></div><div class="equipment">{#each runtime?.equipment ?? ['cannon', 'scanner'] as item}<span>{item}</span>{/each}</div></div>{/each}{:else}<p class="empty">No robot boxes registered.</p>{/if}
        </div>
        <div class="event-feed">
          <div class="eyebrow">LIVE FEED</div>
          {#if snapshot}
            {#each latestFeed as entry (entry.id)}<div class="event-row" data-tone={entry.tone}><span>{entry.label}</span><p>{entry.text}</p></div>{:else}<p class="empty">No combat yet — fire opens the feed.</p>{/each}
          {:else}
            <p class="empty">Feed opens when the match starts.</p>
          {/if}
        </div>
        <div class="cloud-stack"><div class="eyebrow">CONTROL PLANE</div><div class="cloud-row"><span>WS</span><strong>Agent gateway</strong><i>{socketState === 'live' ? 'LIVE' : '--'}</i></div><div class="cloud-row"><span>SQS</span><strong>Match jobs</strong><i>{cloud?.status === 'ready' ? 'READY' : '--'}</i></div><div class="cloud-row"><span>DDB</span><strong>State + results</strong><i>{cloud?.status === 'ready' ? 'READY' : '--'}</i></div><p>{cloud?.provider ?? 'Connecting to Floci…'} · WorkOS AuthKit login.</p></div>
      </aside>
    </section>
  {/if}
</main>
