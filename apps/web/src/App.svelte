<script lang="ts">
  import { onMount } from 'svelte';
  import type { User } from '@workos-inc/authkit-js';
  import Arena from './lib/Arena.svelte';
  import ArenaV2 from './lib/ArenaV2.svelte';
  import BoxConsole from './lib/BoxConsole.svelte';
  import PortalPage from './lib/PortalPage.svelte';
  import { parseRoute, type Route } from './lib/router';
  import { api, setTokenProvider } from './lib/api';
  import { accessToken, authConfigured, clearRedirectCallback, initializeAuth, redirectCallbackPending, signIn, signOut } from './lib/auth';
  import { isNewerSnapshot, type AgentEnrollment, type ArenaEvent, type CloudStatus, type Match, type RobotBox, type RobotState, type Snapshot, type Team } from './lib/types';

  function routeFromLocation() { return parseRoute(window.location.hash); }
  function go(path: string) { window.location.hash = path; }

  let view = $state<Route>(routeFromLocation());
  let menuOpen = $state(false);
  let workspaceRoute = $derived(view.name === 'home' || view.name === 'workspace' || view.name === 'v2');
  let workspacePanel = $derived(view.panel ?? (view.name === 'v2' && view.parameter ? 'match' : 'code'));
  let matchesActive = $derived((workspaceRoute && (workspacePanel === 'match' || workspacePanel === 'results')) || ['matches', 'spectate', 'match', 'match-detail'].includes(view.name));
  let docsActive = $derived(view.name === 'sdk' || view.name === 'api-docs');
  let pageTitle = $derived(matchesActive ? 'Matches' : docsActive ? 'Documentation' : view.name === 'box' ? 'SSH & runtime' : workspaceRoute ? 'Workspace' : 'Robot Arena');
  let team = $state<Team>('red');
  let displayName = $state('Ada');
  let startCommand = $state('lua main.lua');
  let match = $state<Match | null>(null);
  let enrollment = $state<AgentEnrollment | null>(null);
  // The arena uses every server tick. The surrounding HTML refreshes less often
  // so roster cards and the event feed cannot interrupt canvas frames.
  let snapshot = $state.raw<Snapshot | null>(null);
  let arenaSnapshot = $state.raw<Snapshot | null>(null);
  let pendingSnapshot: Snapshot | null = null;
  let snapshotTimer: ReturnType<typeof setTimeout> | null = null;
  let lastSnapshotRender = 0;
  let cloud = $state<CloudStatus | null>(null);
  let checkingServices = $state(true);
  let servicesError = $state('');
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
  let arenaObstacles: Snapshot['obstacles'] = [];

  interface FeedEntry {
    id: string;
    tone: 'ok' | 'bad';
    label: string;
    text: string;
  }

  interface StreakBanner {
    robotId: string;
    name: string;
    streak: string;
    kills: number;
  }

  let hasRed = $derived(match?.robots.some((robot) => robot.team === 'red') ?? false);
  let hasBlue = $derived(match?.robots.some((robot) => robot.team === 'blue') ?? false);
  let isSolo = $derived(match?.mode === 'solo');
  let isSquad = $derived(match?.mode === 'squad');
  let allConnected = $derived(match?.robots.every((robot) => connectedAgents[robot.robotId]) ?? false);
  // Solo free-for-all rosters have no red/blue sides; a start needs at least
  // one robot registered there.
  let canStart = $derived(match?.status === 'lobby' && (isSolo ? (match?.robots.length ?? 0) > 0 : hasRed && hasBlue) && allConnected);
  let myRobot = $derived(match?.robots.find((robot) => robot.ownerBoxId === robotBox?.boxId) ?? null);
  let latestFeed = $derived(feed.slice(-14).reverse());
  let announcements = $derived.by(() => {
    const list = [...(snapshot?.announcements ?? [])];
    if (snapshot?.overtime && !list.includes('OVERTIME')) list.push('OVERTIME');
    return list;
  });

  let streakBanner = $state<StreakBanner | null>(null);
  let streakTimer: ReturnType<typeof setTimeout> | null = null;
  let streakMatchId = '';
  let activeStreak: { robotId: string; streak: string } | null = null;
  let seenStreakEvents = new Set<string>();
  let dismissedStreaks = new Set<string>();

  function streakPhrase(streak: string) {
    return streak === 'UNSTOPPABLE' ? 'IS UNSTOPPABLE' : streak === 'LEGENDARY' ? 'HAS GONE LEGENDARY' : `IS ON A ${streak}`;
  }

  function streakRobotName(robotId: string) {
    return snapshot?.robots.find((entry) => entry.robotId === robotId)?.name ?? robotName(robotId);
  }

  function clearStreakBanner() {
    if (streakTimer) clearTimeout(streakTimer);
    streakTimer = null;
    streakBanner = null;
  }

  function showStreakBanner(matchId: string, robotId: string, streak: string, kills: number) {
    if (streakTimer) clearTimeout(streakTimer);
    activeStreak = { robotId, streak };
    streakBanner = { robotId, name: streakRobotName(robotId), streak, kills };
    streakTimer = setTimeout(() => {
      streakTimer = null;
      dismissedStreaks.add(`${matchId}:${robotId}:${streak}`);
      streakBanner = null;
    }, 4500);
  }

  $effect(() => {
    const current = snapshot;
    if (!current) return;
    if (streakMatchId !== current.matchId) {
      streakMatchId = current.matchId;
      seenStreakEvents.clear();
      dismissedStreaks.clear();
      activeStreak = null;
      clearStreakBanner();
    }
    (current.events ?? []).forEach((event, index) => {
      if (event.type !== 'kill_streak' || !event.robotId) return;
      const key = `${current.sequence}:${index}`;
      if (seenStreakEvents.has(key)) return;
      seenStreakEvents.add(key);
      showStreakBanner(current.matchId, event.robotId, event.message ?? 'RAMPAGE', event.value ?? 0);
    });
    const streak = activeStreak;
    if (streak) {
      const robot = current.robots.find((entry) => entry.robotId === streak.robotId);
      if (!robot || !robot.alive || robot.streakName !== streak.streak) {
        activeStreak = null;
        clearStreakBanner();
      }
    }
    if (!streakBanner) {
      const streaking = current.robots.find((entry) => entry.alive && !!entry.streakName && !dismissedStreaks.has(`${current.matchId}:${entry.robotId}:${entry.streakName}`));
      if (streaking?.streakName) showStreakBanner(current.matchId, streaking.robotId, streaking.streakName, streaking.killStreak ?? 0);
    }
  });

  function robotName(robotId?: string) {
    return match?.robots.find((robot) => robot.robotId === robotId)?.displayName ?? robotId?.slice(0, 6) ?? '?';
  }

  function teamGlyph(team: string) {
    return team === 'red' ? 'R' : team === 'blue' ? 'B' : 'S';
  }

  function teamClass(team: string) {
    return team === 'red' || team === 'blue' ? team : 'solo';
  }

  function feedEntry(seq: number, index: number, event: ArenaEvent): FeedEntry {
    const label = event.type.replace(/_/g, ' ').toUpperCase();
    const fatal = event.type === 'robot_destroyed' || event.type === 'robot_failed' || event.type === 'robot_withdrawn';
    let text = event.message ?? '';
    if (event.type === 'shot_fired') text = `${robotName(event.robotId)} fired`;
    else if (event.type === 'hit') text = `${robotName(event.robotId)} hit ${robotName(event.targetId)} for ${event.damage} dmg`;
    else if (event.type === 'critical_hit') text = `${robotName(event.robotId)} CRIT ${robotName(event.targetId)} for ${event.damage} dmg`;
    else if (event.type === 'supply_drop') text = `supply drop: ${event.message ?? 'bonus item'}`;
    else if (event.type === 'robot_destroyed') text = `${robotName(event.robotId)} destroyed by ${robotName(event.targetId)}`;
    else if (event.type === 'robot_failed') text = `${robotName(event.robotId)} script failed`;
    else if (event.type === 'robot_withdrawn') text = `${robotName(event.robotId)} withdrew from the match`;
    else if (event.type === 'kill_streak' && event.message) text = `${robotName(event.robotId)} ${streakPhrase(event.message)}`;
    else if (event.type === 'item_picked_up' || event.type === 'pickup') text = `${robotName(event.robotId)} picked up ${event.itemType ?? 'item'}`;
    else if (event.type === 'agent_disconnected') text = `${robotName(event.robotId)} disconnected`;
    return { id: `${seq}:${index}:${label}`, tone: fatal ? 'bad' : 'ok', label, text };
  }

  // Rebuild the arena view from the stored match so a refresh during or after
  // a match shows the real session state instead of an empty "waiting" arena.
  // Live snapshots carry higher sequences, so they replace this once playing.
  function snapshotFromMatch(source: Match): Snapshot {
    const lanes: Record<string, number> = {};
    const summaries = source.robotSummaries ?? [];
    const robots: RobotState[] = source.robots.map((robot) => {
      const summary = summaries.find((entry) => entry.robotId === robot.robotId);
      const lane = lanes[robot.team] ?? 0;
      lanes[robot.team] = lane + 1;
      return {
        robotId: robot.robotId,
        name: robot.displayName,
        team: robot.team,
        x: source.arenaWidth ? (robot.team === 'red' ? 0.25 : 0.75) * source.arenaWidth : robot.team === 'red' ? 200 : 600,
        y: 110 + lane * 110,
        heading: 0,
        hp: summary?.hp ?? 100,
        cooldown: 0,
        alive: summary?.alive ?? true,
        failed: summary?.failed ?? false,
        connected: false,
        avgResponseMs: summary?.avgResponseMs ?? 0,
        lastResponseMs: 0,
        computeMs: 0,
        memoryMb: 0,
        equipment: summary?.equipment,
        itemsPickedUp: summary?.itemsPickedUp,
      };
    });
    const events = source.eventSummary ?? [];
    return {
      type: 'snapshot',
      version: 1,
      matchId: source.matchId,
      sequence: 0,
      tick: events.length ? events[events.length - 1].tick : 0,
      status: source.status === 'finished' ? 'finished' : 'running',
      winnerTeam: source.winnerTeam,
      robots,
      projectiles: [],
      events,
      width: source.arenaWidth ?? 800,
      height: source.arenaHeight ?? 500,
      mapId: source.mapId,
    };
  }

  function hydrateMatchView(source: Match) {
    if (source.status !== 'finished' && source.status !== 'failed') return;
    if (arenaSnapshot?.matchId === source.matchId) return;
    publishSnapshot(snapshotFromMatch(source));
    feed = (source.eventSummary ?? []).map((event, index) => feedEntry(event.sequence, index, event));
  }

  function publishSnapshot(next: Snapshot) {
    arenaSnapshot = next;
    const now = performance.now();
    const mustRender = !snapshot || snapshot.matchId !== next.matchId || snapshot.status !== next.status || now - lastSnapshotRender >= 200;
    if (mustRender) {
      if (snapshotTimer) clearTimeout(snapshotTimer);
      snapshotTimer = null;
      pendingSnapshot = null;
      lastSnapshotRender = now;
      snapshot = next;
      return;
    }
    pendingSnapshot = next;
    if (!snapshotTimer) {
      snapshotTimer = setTimeout(() => {
        snapshotTimer = null;
        if (!pendingSnapshot) return;
        snapshot = pendingSnapshot;
        pendingSnapshot = null;
        lastSnapshotRender = performance.now();
      }, Math.max(0, 200 - (now - lastSnapshotRender)));
    }
  }

  onMount(() => {
    const onHash = () => { view = routeFromLocation(); menuOpen = false; void loadRouteMatch(); };
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') menuOpen = false; };
    window.addEventListener('hashchange', onHash);
    window.addEventListener('keydown', onKey);
    void refreshServices();
    void initialize();
    return () => {
      window.removeEventListener('hashchange', onHash);
      window.removeEventListener('keydown', onKey);
      socket?.close();
      clearStreakBanner();
      if (snapshotTimer) clearTimeout(snapshotTimer);
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
    const matchId = view.parameter && (view.name === 'match' || view.name === 'match-detail') ? view.parameter : new URL(window.location.href).searchParams.get('match');
    if (matchId) {
      try {
        match = await api.getMatch(matchId);
        if (match.engineVersion === 4) { go(`/v2/${matchId}`); return; }
        hydrateMatchView(match);
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
        setError('This match link is no longer valid. The match may have ended or the stack restarted. Create a new match to continue.');
      }
    }
  }

  async function refreshServices() {
    checkingServices = true;
    try {
      cloud = await api.cloudStatus();
      if (error === servicesError) error = '';
      servicesError = '';
    } catch (failure) {
      cloud = null;
      setError(failure);
      servicesError = error;
    } finally {
      checkingServices = false;
    }
  }

  async function loadRouteMatch() {
    if (!view.parameter || (view.name !== 'match' && view.name !== 'match-detail') || view.parameter === match?.matchId) return;
    try { match = await api.getMatch(view.parameter); if (match.engineVersion === 4) { go(`/v2/${match.matchId}`); return; } hydrateMatchView(match); if (view.name === 'match') connect(view.parameter); }
    catch (failure) { setError(failure); }
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
      window.location.hash = `/match/${match.matchId}`;
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

  // Mid-match concession: the server destroys the robot on the next tick and
  // the match resolves by normal elimination rules.
  const withdrawFromMatch = () => withPending('withdraw', async () => {
    if (!match) return;
    error = '';
    await api.withdrawFromMatch(match.matchId);
  });

  // Box console escape hatch: release the box from whatever match binds it.
  // A running match is conceded; a lobby match drops the registration. A
  // marker referencing a vanished match is stale — the next box read clears
  // it server-side, so refresh instead of surfacing a raw store error.
  const withdrawFromBox = () => withPending('box-withdraw', async () => {
    if (!robotBox?.activeMatchId) return;
    error = '';
    let target: Match;
    try {
      target = await api.getMatch(robotBox.activeMatchId);
    } catch {
      await refreshBox();
      throw new Error('bound match no longer exists; box binding was stale and has been cleared');
    }
    if (target.status === 'running') await api.withdrawFromMatch(target.matchId);
    else if (target.status === 'lobby') await api.withdrawRobot(target.matchId);
    else throw new Error('match is no longer active; refresh the box');
    await refreshBox();
  });

  // Popup escape hatch for the "box already has an active robot" fault: the
  // server drops a lobby registration, concedes a running match, or clears a
  // stale binding, then the box state refreshes so queueing works again.
  const exitActiveMatch = () => withPending('exit-active', async () => {
    error = '';
    await api.releaseBox();
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
    window.location.hash = `/match/${found.matchId}`;
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

  const copyInvite = () => {
    const url = new URL(window.location.href);
    if (match) url.hash = `/match/${match.matchId}`;
    return copyFeedback('link', url.toString());
  };

  function bytes(value = 0) { return `${(value / (1024 * 1024)).toFixed(1)} MB`; }

  function connect(matchId: string) {
    socket?.close();
    arenaObstacles = [];
    socketState = 'connecting';
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${scheme}//${window.location.host}/ws/matches/${encodeURIComponent(matchId)}`);
    socket.onopen = () => { socketState = 'live'; };
    socket.onclose = () => { socketState = 'offline'; };
    socket.onerror = () => { socketState = 'offline'; };
    socket.onmessage = ({ data }) => {
      const event = JSON.parse(data);
      if (event.type === 'arena_layout') {
        arenaObstacles = event.obstacles ?? [];
        const current = arenaSnapshot;
        if (current && current.matchId === event.matchId) publishSnapshot({ ...current, obstacles: arenaObstacles });
        return;
      }
      if (event.type === 'snapshot' && isNewerSnapshot(arenaSnapshot, event)) {
        event.obstacles = event.obstacles ?? arenaObstacles;
        publishSnapshot(event);
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

<svelte:head><title>{pageTitle} · Robot Arena</title></svelte:head>

<div class="shell">
  <a class="skip-link" href="#main-content" onclick={(event) => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>Skip to content</a>
  <header class="topbar">
    <a class="brand" href="#/workspace" aria-label="Robot Arena workspace"><span class="brand-mark" aria-hidden="true">&gt;_</span><span><strong>Robot Arena</strong><small>Code. Run. Compete.</small></span></a>
    <nav class="main-nav" aria-label="Primary">
      <a class:active={workspaceRoute && !matchesActive} aria-current={workspaceRoute && !matchesActive ? 'page' : undefined} href="#/workspace">Workspace</a>
      <a class:active={matchesActive} aria-current={matchesActive ? 'page' : undefined} href="#/workspace/matches">Matches</a>
      <a class:active={docsActive} aria-current={docsActive ? 'page' : undefined} href="#/docs/sdk">Docs</a>
    </nav>
    <div class="account">
      <div class="system-state" class:bad={!checkingServices && cloud?.status !== 'ready'} class:checking={checkingServices} title="Local services"><span aria-hidden="true"></span><span class="service-label" role="status">{checkingServices ? 'Checking services' : cloud?.status === 'ready' ? 'Services ready' : 'Services unavailable'}</span>{#if !checkingServices && cloud?.status !== 'ready'}<button class="retry-services" onclick={refreshServices} aria-label="Retry service connection">Retry</button>{/if}</div>
      <details class="more-nav" bind:open={menuOpen}>
        <summary aria-label="More navigation">More <span aria-hidden="true">⌄</span></summary>
        <nav class="more-menu" aria-label="Additional navigation">
          <span class="menu-label">Explore</span>
          <a href="#/matches">Match history</a>
          <a href="#/spectate">Watch live</a>
          <span class="menu-label">Advanced</span>
          <a href="#/box" aria-current={view.name === 'box' ? 'page' : undefined}>SSH &amp; runtime <i class="nav-dot" class:ok={robotBox?.status === 'running'} class:bad={robotBox?.status === 'failed' || !!robotBox?.error} aria-hidden="true"></i></a>
          <a href="#/settings" aria-current={view.name === 'settings' ? 'page' : undefined}>Account settings</a>
          <a href="#/docs/api">API reference</a>
          {#if cloud?.status !== 'ready'}<button onclick={refreshServices} disabled={checkingServices}>{checkingServices ? 'Checking services…' : 'Retry service connection'}</button>{/if}
        </nav>
      </details>
      {#if user}<button class="auth-button" onclick={signOut} title={`Sign out of ${user.email}`}>{user.firstName ?? 'Account'} <span>· Sign out</span></button>{:else}<button class="auth-button sign-in" disabled={signingIn} onclick={handleSignIn}>{signingIn ? 'Signing in…' : 'Sign in'}</button>{/if}
    </div>
  </header>

  {#if error}<div class="error-banner" role="alert"><span>FAULT</span>{error}{#if error.includes('box already has an active robot')}<button onclick={exitActiveMatch} disabled={pending['exit-active']}>{pending['exit-active'] ? 'RELEASING…' : 'EXIT ACTIVE MATCH'}</button>{/if}<button onclick={() => (error = '')}>DISMISS</button></div>{/if}

  <main id="main-content" tabindex="-1">
  {#if workspaceRoute}
 <ArenaV2 signedIn={!!user} matchId={view.parameter ?? ''} initialPanel={workspacePanel} onSignIn={handleSignIn}/>
 {:else if view.name === 'box'}
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
      onWithdrawMatch={withdrawFromBox}
    />
  {:else if view.name === 'match'}
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
            <div class="quota"><span>{bytes(robotBox.usageBytes)} / {bytes(robotBox.limits.storageBytes)}</span><button onclick={() => go('/box')}>MANAGE BOX →</button></div>
          </div>
        {:else}
          <div class="box-card"><div class="eyebrow">SSH BOX</div><strong>NOT PROVISIONED</strong><p>Open the box console to provision a persistent container for your account.</p><button class="save-key" onclick={provisionBox} disabled={busy || pending.provision}>{pending.provision ? 'PROVISIONING…' : 'PROVISION BOX'}</button></div>
        {/if}

        {#if user && robotBox?.activeMatchId && match}
          {#if robotBox.activeMatchId === match.matchId}
            <div class="box-match-note" data-tone="ok"><span>IN MATCH</span>Your box agent is committed to this match.</div>
          {:else}
            <div class="box-match-note" data-tone="bad"><span>BOX BUSY</span>Your box is already in <a href={`#/match/${robotBox.activeMatchId}`}>another match</a>. Registering here is blocked until it ends.</div>
          {/if}
        {/if}

        {#if !match}
          <label>Match code<input bind:value={joinCode} maxlength="300" placeholder="Paste match ID or invite link" /></label>
          <button class="join-match" onclick={joinMatch} disabled={busy || pending.join || !user || !joinCode.trim()}>{working('join', 'JOINING…') ?? 'JOIN MATCH BY CODE'}</button>
        {/if}

        {#if match}<div class="match-id"><span>MATCH ID</span><code>{match.matchId.slice(0, 13)}</code><button onclick={copyInvite} disabled={pending.link}>{copied === 'link' ? 'COPIED ✓' : 'COPY LINK'}</button></div>{/if}
        <label>Robot name<input bind:value={displayName} maxlength="32" disabled={match?.status !== 'lobby' && !!match} /></label>
        {#if !isSolo}
          <fieldset disabled={match?.status !== 'lobby' && !!match}><legend>Team{isSquad ? ' · joining replaces one bot' : ''}</legend><div class="team-switch"><button class:active={team === 'red'} onclick={() => (team = 'red')}>RED</button><button class:active={team === 'blue'} onclick={() => (team = 'blue')}>BLUE</button></div></fieldset>
        {/if}
        <label>Start command<input bind:value={startCommand} maxlength="256" disabled={match?.status !== 'lobby' && !!match} /></label>

        <div class="facts"><div><span>MODE</span><strong>{(match?.mode ?? 'duel').toUpperCase()}</strong></div><div><span>TICK</span><strong>{snapshot?.tick ?? 0}</strong></div><div><span>VIEW STREAM</span><strong class:live={socketState === 'live'}>{socketState.toUpperCase()}</strong></div></div>

        {#if !match}<button class="deploy" onclick={createMatch} disabled={busy || pending.create || !user || !robotBox?.keyFingerprint}>{working('create', 'CREATING…') ?? 'CREATE MATCH'} <span>↗</span></button>
        {:else if match.status === 'lobby'}<div class="lobby-actions">
            <button class="deploy" onclick={registerRobot} disabled={busy || pending.register || !user || !robotBox?.keyFingerprint || !!myRobot}>{myRobot ? `REGISTERED AS ${myRobot.team.toUpperCase()}` : (working('register', 'REGISTERING…') ?? 'REGISTER BOX')} <span>↗</span></button>
            <button class="start-match" onclick={startMatch} disabled={busy || pending.start || !canStart}>{working('start', 'STARTING…') ?? 'START NOW (AUTO WHEN READY)'}</button>
            {#if myRobot}<button class="withdraw" onclick={withdrawRobot} disabled={busy || pending.withdraw}>{working('withdraw', 'WITHDRAWING…') ?? 'WITHDRAW ROBOT'}</button>{/if}
          </div>
        {:else}<div class="match-result" class:finished={match.status === 'finished'}><span>{match.status === 'finished' ? 'WINNER' : 'MATCH STATE'}</span><strong>{match.winnerTeam?.toUpperCase() ?? match.status.toUpperCase()}</strong></div>
          {#if match.status !== 'running'}<a class="secondary-action queue-again" href="#/play">{match.status === 'finished' ? 'QUEUE AGAIN' : 'BACK TO QUEUE'} →</a>{/if}
          {#if match.status === 'running' && myRobot}<button class="withdraw" onclick={withdrawFromMatch} disabled={busy || pending.withdraw}>{working('withdraw', 'WITHDRAWING…') ?? 'WITHDRAW FROM MATCH'}</button>{/if}{/if}
      </aside>

      <section class="arena-panel">
        <div class="panel-head"><div><span class="status-dot"></span>ARENA // LIVE</div><span>{snapshot?.status.toUpperCase() ?? 'WAITING FOR AGENTS'}</span></div>
        <div class="arena-stage">
          <Arena snapshot={arenaSnapshot} />
          {#if announcements.length}
            <div class="announcer-chips" role="status">{#each announcements as note (note)}<span class="announcer-chip" data-note={note}>{note}</span>{/each}</div>
          {/if}
          {#if streakBanner}
            <div class="streak-banner" role="status"><b>{streakBanner.name}</b><span>{streakPhrase(streakBanner.streak)}</span>{#if streakBanner.kills > 0}<i>×{streakBanner.kills}</i>{/if}</div>
          {/if}
        </div>
        <div class="stats-strip">
          {#each snapshot?.robots ?? [] as robot}<div><span>{robot.name}</span><strong>{robot.hp}<small> HP</small></strong><i>{robot.avgResponseMs?.toFixed(1) ?? '0.0'} ms avg</i></div>{/each}
          {#if !snapshot}<div><span>RESPONSE</span><strong>--</strong><i>agent telemetry</i></div><div><span>HEALTH</span><strong>--</strong><i>waiting for match</i></div>{/if}
        </div>
        {#if snapshot?.status === 'finished'}
          <div class="arena-recap" role="status"><div><span>MATCH COMPLETE</span><h2>{snapshot.winnerTeam ? `${snapshot.winnerTeam.toUpperCase()} WINS` : match?.status === 'failed' ? 'MATCH FAILED' : 'DRAW'}</h2><p>{snapshot.tick} ticks · {snapshot.robots.filter((robot) => !robot.alive).length} destroyed · {snapshot.robots.reduce((sum, robot) => sum + (robot.itemsPickedUp ?? 0), 0)} items · {feed.filter((entry) => entry.label === 'HIT').reduce((sum, entry) => sum + Number(entry.text.match(/for (\d+)/)?.[1] ?? 0), 0)} damage</p></div><a href={`#/match/${match?.matchId}/detail`}>FULL RECAP</a></div>
        {/if}
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
          {#if match?.robots.length}{#each match.robots as robot}{@const runtime = snapshot?.robots.find((entry) => entry.robotId === robot.robotId)}<div class="robot-card"><div class="roster-row"><b class={teamClass(robot.team)}>{teamGlyph(robot.team)}</b><strong>{robot.displayName}</strong><span class:live={connectedAgents[robot.robotId]}>{connectedAgents[robot.robotId] ? 'CONNECTED' : 'OFFLINE'}</span></div><div class="hp-track" aria-label="hit points"><i class:low={(runtime?.hp ?? 100) <= 30} style="width: {runtime?.hp ?? 100}%"></i></div><div class="metric-grid"><div><span>HP</span><strong>{runtime?.hp ?? 100}</strong></div><div><span>AVG</span><strong>{runtime?.avgResponseMs?.toFixed(1) ?? '--'} ms</strong></div><div><span>COMPUTE</span><strong>{runtime?.computeMs?.toFixed(2) ?? '--'} ms</strong></div><div><span>MEMORY</span><strong>{runtime?.memoryMb?.toFixed(0) ?? '--'} MB</strong></div></div><div class="equipment">{#each runtime?.equipment ?? ['cannon', 'scanner'] as item}<span>{item}</span>{/each}</div></div>{/each}{:else}<p class="empty">No robot boxes registered.</p>{/if}
        </div>
        <div class="event-feed">
          <div class="eyebrow">LIVE FEED</div>
          {#if snapshot}
            {#each latestFeed as entry (entry.id)}<div class="event-row" data-tone={entry.tone}><span>{entry.label}</span><p>{entry.text}</p></div>{:else}<p class="empty">No combat yet. Firing opens the feed.</p>{/each}
          {:else}
            <p class="empty">Feed opens when the match starts.</p>
          {/if}
        </div>
        <div class="cloud-stack"><div class="eyebrow">CONTROL PLANE</div><div class="cloud-row"><span>WS</span><strong>Agent gateway</strong><i>{socketState === 'live' ? 'LIVE' : '--'}</i></div><div class="cloud-row"><span>SQS</span><strong>Match jobs</strong><i>{cloud?.status === 'ready' ? 'READY' : '--'}</i></div><div class="cloud-row"><span>DDB</span><strong>State + results</strong><i>{cloud?.status === 'ready' ? 'READY' : '--'}</i></div><p>{cloud?.provider ?? 'Connecting to Floci…'} · WorkOS AuthKit login.</p></div>
      </aside>
    </section>
  {:else}
    <PortalPage route={view} {user} {match} {cloud} box={robotBox} onSignIn={handleSignIn} onCreateMatch={createMatch} onReleaseBox={exitActiveMatch} />
  {/if}
  </main>
</div>
