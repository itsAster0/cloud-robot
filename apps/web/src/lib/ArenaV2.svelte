<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { api, request } from './api';
  import type { Match, Snapshot, RobotState, RobotBox } from './types';
  import WorkspaceEditor from './WorkspaceEditor.svelte';
  import WorldView from './WorldView.svelte';
  import VirtualRoster from './VirtualRoster.svelte';
  import MapPreview, { type MapPreviewData } from './MapPreview.svelte';
  import MatchOverview from './MatchOverview.svelte';
  import ArenaScoreboard from './ArenaScoreboard.svelte';
  import ThingInspector from './ThingInspector.svelte';
  import KillFeed from './KillFeed.svelte';
  import type { ArenaEvent } from './types';
  import type { Picked } from './picking';
  import MatchSetup from './MatchSetup.svelte';
  import LoadoutBuilder from './LoadoutBuilder.svelte';
  import RobotInspector from './RobotInspector.svelte';
  import { killsLeader } from './matchStats';
  type Panel = 'code' | 'build' | 'match' | 'debug' | 'results';
  let { signedIn, matchId = '', initialPanel = 'code', onSignIn }: { signedIn: boolean; matchId?: string; initialPanel?: Panel; onSignIn?: () => void } = $props();
  const stages: { id: Panel; label: string; href: string }[] = [
    { id: 'code', label: 'Code', href: '#/workspace' },
    { id: 'build', label: 'Loadout', href: '#/workspace/build' },
    { id: 'debug', label: 'Test & debug', href: '#/workspace/test' },
    { id: 'match', label: 'Matches', href: '#/workspace/matches' },
    { id: 'results', label: 'Results', href: '#/workspace/results' },
  ];
  let box = $state<RobotBox | null>(null), boxLoading = $state(false);
  let savedSource = $state(''), confirmReload = $state(false);
  let syntaxResult = $state<{ ok: boolean; message: string } | null>(null);
  let recentMatches = $state<Match[]>([]), matchesLoading = $state(false), matchesError = $state('');
  let matchPreview = $state<MapPreviewData | null>(null);
  let draftKey = $derived(box ? `robot-arena:workspace:${box.boxId}` : '');
  function showPanel(next: Panel) { panel = next; window.location.hash = stages.find(stage => stage.id === next)!.href; }
  function prepareSandbox() { mode = 'sandbox'; modeDefaults(); showPanel('debug'); }
  function beforeUnload(event: BeforeUnloadEvent) { if (dirty) { event.preventDefault(); event.returnValue = ''; } }
  function persistDraft() {
    if (!draftKey || !editorLoaded) return;
    try {
      if (dirty) sessionStorage.setItem(draftKey, JSON.stringify({ source, revision, savedSource }));
      else sessionStorage.removeItem(draftKey);
    } catch { /* The unload warning still protects drafts when storage is unavailable. */ }
  }
  async function refreshBox() {
    boxLoading = true;
    try {
      const result = await api.getBox();
      if (!signedIn || disposed) return;
      box = result;
      if (!editorLoaded) {
        try {
          const raw = sessionStorage.getItem(`robot-arena:workspace:${result.boxId}`);
          if (raw) {
            const draft = JSON.parse(raw);
            if (typeof draft.source === 'string' && typeof draft.savedSource === 'string' && typeof draft.revision === 'string' && draft.revision.length === 64) {
              source = draft.source; savedSource = draft.savedSource; revision = draft.revision; editorLoaded = true;
              message = 'Restored your unsaved draft from this browser tab. Save it before registering a robot.';
            }
          }
        } catch { /* A corrupt browser draft must not block the workspace. */ }
      }
    } catch { box = null; }
    finally { boxLoading = false; }
  }
  async function refreshMatches() {
    matchesLoading = true; matchesError = '';
    try { recentMatches = (await api.listMatches(undefined, 100)).matches.filter(item => item.engineVersion === 4); }
    catch (e) { matchesError = e instanceof Error ? e.message : String(e); }
    finally { matchesLoading = false; }
  }
  async function openListed(item: Match) { idInput = item.matchId; await open(); if (match?.matchId === item.matchId) showPanel(item.status === 'finished' ? 'results' : 'match'); }

  type Loadout = { chassis: string; weapon: string; modules: string[]; utilities: string[] };
  let mode = $state('br-solo'), capacity = $state(64), size = $state(35), duration = $state(1080), liveEdit = $state(false);
  let siteCount = $state(64), coverPerSite = $state(8), lootPerSite = $state(16), seed = $state(0);
  let chassis = $state('generalist'), weapon = $state('plasma'), modules = $state<string[]>([]), utilities = $state<string[]>([]);
  let name = $state('My robot'), squad = $state(''), error = $state(''), message = $state(''), busy = $state(false);
  let match = $state<Match | null>(null), snapshot = $state.raw<Snapshot | null>(null), selected = $state(''), idInput = $state('');
  let source = $state(''), revision = $state(''), editorLoaded = $state(false), privateView = $state(false), paused = $state(false);
  let edit = $state('{"expectedRevision":1,"effectiveTick":100,"obstacles":[],"removeObstacles":[],"containers":[],"transit":[]}');
  let preview = $state(''), panel = $state<Panel>('code');
  let replayTick=$state(0),replayEnd=$state(0),trace=$state('');
  let seekSequence=0, previewSequence=0;
  let templates = $state<{name:string;description:string}[]>([]);
  let mapPreview = $state<MapPreviewData|null>(null), previewLoading = $state(false);
  let previewTimer: ReturnType<typeof setTimeout> | undefined;
  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let reconnectAttempt = 0, disposed = false, viewerEpoch = $state(0);
  let connectionStatus = $state('No match open');
  let overview = $state.raw<RobotState[]>([]);
  let publicLayout: Pick<Snapshot, 'obstacles' | 'transit' | 'hazards' | 'sites'> = {};
  function cameraRegion(region: {x:number;y:number;width:number;height:number}) { if(socket?.readyState===WebSocket.OPEN) socket.send(JSON.stringify(region)); }
  const chassisCosts: Record<string, number> = { scout: 10, generalist: 15, heavy: 25 };
  const weapons: Record<string, number> = { plasma: 10, machine_gun: 15, shotgun: 15, cannon: 20, railgun: 25, grenade: 20, incendiary: 15, cryo: 15, emp: 20 };
  const passive = ['reinforced_plating', 'optics', 'capacitor', 'cooling_system', 'mobility_tuning', 'shield_reservoir'];
  const utilityCosts: Record<string, number> = { cloak_emitter: 15, mine_dispenser: 10, smoke_projector: 10, repair_field: 15 };
  let cost = $derived((chassisCosts[chassis] ?? 0) + (weapons[weapon] ?? 0) + modules.length * 10 + utilities.reduce((n, id) => n + utilityCosts[id], 0));
  let dirty = $derived(editorLoaded && source !== savedSource);
  // Public match data hides owners, so "mine" comes from the box binding.
  let registeredId = $state('');
  let myRobotId = $derived(
    registeredId && match?.robots.some(r => r.robotId === registeredId) ? registeredId
    : box?.activeRobotId && match?.robots.some(r => r.robotId === box?.activeRobotId) ? box.activeRobotId : '');
  // Keep the box binding fresh while a match is open (joins from other pages).
  $effect(() => {
    if (!signedIn || !match || match.status === 'finished' || match.status === 'failed') return;
    const timer = setInterval(() => void refreshBox(), 5000);
    return () => clearInterval(timer);
  });
  // Winner teams are IDs; show the robots on that team instead.
  function winnerName(m: Match) {
    if (!m.winnerTeam || m.winnerTeam === 'draw') return m.winnerTeam === 'draw' ? 'Nobody (draw)' : '';
    const names = [...new Set((m.robotSummaries ?? []).filter(r => r.team === m.winnerTeam).map(r => r.name))];
    if (!names.length) names.push(...(m.robots ?? []).filter(r => r.team === m.winnerTeam).map(r => r.displayName));
    return names.length ? names.slice(0, 3).join(', ') : m.winnerTeam;
  }
  let welcome = $state('');
  // A finished arena session hands viewers to the next one.
  let nextArenaWaiting = $state(false);
  async function nextArena() {
    nextArenaWaiting = true;
    try {
      for (let i = 0; i < 20; i++) {
        const a = await api.getArena().catch(() => null);
        if (a && a.match.matchId !== match?.matchId && a.match.status === 'running') { window.location.hash = `#/v2/${a.match.matchId}`; return; }
        await new Promise(r => setTimeout(r, 1500));
      }
      message = 'The next session is still starting. Try again in a few seconds.';
    } finally { nextArenaWaiting = false; }
  }
  // Theater layout: while a match is on screen the map gets the page width
  // and robot details move to a sidebar, like a video page.
  let watching = $derived(!!match && panel !== 'code' && panel !== 'build' && (!!snapshot || match.status === 'running' || match.status === 'finished'));
  // Map thing the viewer clicked (loot, terrain, hazard, site); robots win.
  let picked = $state<Picked | null>(null);
  // Kill feed: merge each snapshot's recent feed, dedupe, keep ~8 seconds.
  let feed = $state<(ArenaEvent & { seen: number })[]>([]);
  function mergeFeed(events: ArenaEvent[] | undefined) {
    if (!events?.length) return;
    const now = Date.now(), key = (e: ArenaEvent) => `${e.tick}:${e.type}:${e.robotId}:${e.targetId}`;
    const known = new Set(feed.map(key));
    const fresh = events.filter(e => !known.has(key(e))).map(e => ({ ...e, seen: now }));
    if (fresh.length) feed = [...fresh.reverse(), ...feed].filter(e => now - e.seen < 8000).slice(0, 6);
  }
  $effect(() => { const t = setInterval(() => { const now = Date.now(); if (feed.some(e => now - e.seen >= 8000)) feed = feed.filter(e => now - e.seen < 8000); }, 1000); return () => clearInterval(t); });
  // Arena ranks by score; other modes by kills.
  let leaderId = $derived.by(() => {
    const robots = overview.length ? overview : snapshot?.robots ?? [];
    if (match?.mode !== 'arena') return killsLeader(robots)?.robotId ?? '';
    let best: RobotState | undefined;
    for (const r of robots) if ((r.score ?? 0) > (best?.score ?? 0)) best = r;
    return best?.robotId ?? '';
  });
  let confirmLeave = $state(false);
  // Frees the box: lobby registrations are dropped and a running robot
  // concedes; the player keeps watching as a spectator.
  async function leaveMatch() {
    await task(async () => {
      await api.releaseBox();
      confirmLeave = false; privateView = false; selected = '';
      await refreshBox();
      if (match) match = await api.getMatch(match.matchId);
      message = 'You left the match. Your box is free for the next one; you can keep watching.';
    });
  }
  let autoSelectedFor = '';
  $effect(() => {
    const id = match?.matchId ?? '';
    if (!id || !myRobotId || autoSelectedFor === id) return;
    autoSelectedFor = id;
    // Follow the player's own robot first; they can click others later.
    untrack(() => { if (!selected) selected = myRobotId; });
  });
  $effect(() => {
    const id = match?.matchId ?? '';
    if (!id) return;
    untrack(() => {
      try {
        if (sessionStorage.getItem('robot-arena:welcome') === id) {
          welcome = id;
          sessionStorage.removeItem('robot-arena:welcome');
          setTimeout(() => document.querySelector('.world')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 600);
        }
      } catch { /* no storage: skip the welcome */ }
    });
  });
  let registered = $derived(!!box && !!match?.robots.some(robot => robot.ownerBoxId === box?.boxId || robot.robotId === box?.activeRobotId));
  let canRegister = $derived(signedIn && box?.status === 'running' && !dirty && cost <= 60);
  let inspected = $derived(selected ? snapshot?.robots.find(r => r.robotId === selected) ?? overview.find(r => r.robotId === selected) : undefined);
  function closeMatch() { clearTimeout(retryTimer); const current = socket; socket = null; current?.close(); match = null; snapshot = null; overview = []; replayEnd = 0; selected = ''; matchPreview = null; idInput = ''; if (window.location.hash.startsWith('#/v2/')) window.location.hash = '#/workspace/matches'; }
  let teamSize = $state(4), botFill = $state(true), botCount = $state(0);
  const teamNames: Record<number, string> = { 2: 'Duo', 3: 'Trio', 4: 'Squad' };
  // Human title for a match, including duo/trio team sizes.
  function matchTitle(m: Match) {
    const size = (m.arenaConfig as { teamSize?: number } | undefined)?.teamSize;
    if (m.mode === 'br-squad' && size) return `${teamNames[size] ?? `${size}-player team`} battle royale`;
    return title(m.mode);
  }
  let lobbyTeams = $derived([...new Set((match?.robots ?? []).filter(r => !r.bot).map(r => r.team))]);
  let lobbyBots = $derived((match?.arenaConfig as { bots?: number } | undefined)?.bots);
  let lobbyCapacity = $derived((match?.arenaConfig as { capacity?: number } | undefined)?.capacity ?? 0);
  let copiedLink = $state(false);
  async function copyLink() {
    if (!match) return;
    try { await navigator.clipboard.writeText(`${location.origin}/#/v2/${match.matchId}`); copiedLink = true; setTimeout(() => copiedLink = false, 2000); } catch { message = `Share this link: ${location.origin}/#/v2/${match.matchId}`; }
  }
  const title = (id: string) => ({ 'br-solo': 'Solo battle royale', 'br-squad': 'Squad battle royale', 'quick-duel': 'Quick duel', sandbox: 'Sandbox', arena: 'The Arena' }[id] ?? id.replace(/_/g, ' '));
  async function task(fn: () => Promise<void>) { busy = true; error = ''; message = ''; try { await fn(); } catch (e) { error = e instanceof Error ? e.message : String(e); } finally { busy = false; } }
  function loadout(): Loadout { return { chassis, weapon, modules: [...modules], utilities: [...utilities] }; }
  function modeDefaults() {
    const practice = mode === 'quick-duel' || mode === 'sandbox';
    capacity = mode === 'quick-duel' ? 2 : mode === 'sandbox' ? 8 : 64;
    duration = practice ? 180 : 1080;
    liveEdit = mode === 'sandbox';
    if (practice) { size = 2; siteCount = 4; coverPerSite = 8; lootPerSite = 16; }
    else { size = 35; siteCount = 64; coverPerSite = 8; lootPerSite = 16; }
  }
  function preset(id: string) { if (id === 'scout') { chassis = 'scout'; weapon = 'machine_gun'; modules = ['optics', 'mobility_tuning']; utilities = ['cloak_emitter']; } else if (id === 'sniper') { chassis = 'generalist'; weapon = 'railgun'; modules = ['optics', 'cooling_system']; utilities = []; } else if (id === 'support') { chassis = 'generalist'; weapon = 'plasma'; modules = ['capacitor']; utilities = ['repair_field', 'smoke_projector']; } else { chassis = 'generalist'; weapon = 'machine_gun'; modules = ['reinforced_plating', 'cooling_system']; utilities = ['mine_dispenser']; } }
  function previewConfig() {
    const large = mode === 'br-solo' || mode === 'br-squad';
    return { mode, capacity, width: large ? 1200 * size : 2400, height: large ? 750 * size : 1500, durationSeconds: duration, liveEdit, siteCount, coverPerSite, lootPerSite, seed, ...(mode === 'br-squad' ? { teamSize } : {}), ...(botFill ? {} : { bots: Math.min(botCount, capacity) }) };
  }
  async function fetchPreview() {
    if (!signedIn) { mapPreview = null; return; }
    const sequence = ++previewSequence; previewLoading = true;
    try { const result = await request<MapPreviewData>('/api/v4/maps/preview', { method: 'POST', body: JSON.stringify(previewConfig()) }); if (sequence === previewSequence) mapPreview = result; }
    catch { if (sequence === previewSequence) mapPreview = null; }
    finally { if (sequence === previewSequence) previewLoading = false; }
  }
  function schedulePreview() { clearTimeout(previewTimer); previewTimer = setTimeout(() => void fetchPreview(), 400); }
  function randomizeSeed() { seed = 1 + Math.floor(Math.random() * 999998); schedulePreview(); }
  async function create() { await task(async () => {
    // Seed 0 means random: materialize it client-side so the instant preview
    // below shows exactly the lobby's map, not another roll.
    if (!seed) seed = 1 + Math.floor(Math.random() * 999998);
    const config = previewConfig();
    match = await request<Match>('/api/v4/matches', { method: 'POST', body: JSON.stringify(config) });
    idInput = match.matchId; privateView = false; matchPreview = null;
    connect(match.matchId);
    const createdId = match.matchId;
    void request<MapPreviewData>('/api/v4/maps/preview', { method: 'POST', body: JSON.stringify(config) }).then(result => { if (match?.matchId === createdId) matchPreview = result; }).catch(() => {});
    message = 'Lobby created. Register your saved script, then start the match.';
    void refreshMatches();
  }); }
  function connect(id: string) { clearTimeout(retryTimer); const previous=socket; socket=null; previous?.close(); viewerEpoch++; connectionStatus="Connecting"; overview=[]; publicLayout={}; snapshot=null;replayEnd=0;replayTick=0;trace=""; const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws/matches/${encodeURIComponent(id)}`); socket = ws;
    ws.onopen=()=>{if(socket===ws){connectionStatus='Connected';}};
    ws.onclose=()=>{if(socket!==ws || disposed || match?.status==='finished')return; connectionStatus='Reconnecting'; retryTimer=setTimeout(()=>{if(!disposed && socket===ws)connect(id);},Math.min(10000,500*2**Math.min(reconnectAttempt++,5)));};
    ws.onmessage = ({ data }) => { if (socket !== ws) return; reconnectAttempt=0; const event = JSON.parse(data); if (event.type === 'snapshot') { if(event.obstacles) publicLayout={obstacles:event.obstacles,transit:event.transit,hazards:event.hazards,sites:event.sites}; if(event.overview) overview=event.overview; mergeFeed(event.feed); if(!privateView) snapshot={...publicLayout,...event}; } if (event.type === 'match_state' || event.type === 'match_finished') { match = event.match; if (event.type === 'match_finished') void loadFinal().catch(e => { error = e instanceof Error ? e.message : String(e); }); } };
    ws.onerror = () => { if(socket===ws) connectionStatus='Connection interrupted'; };
  }
  async function open() { await task(async () => {
    const id = idInput.trim();
    if (!id) throw new Error('Enter a match ID first, or create a lobby in Match settings.');
    const found = await api.getMatch(id);
    if (found.engineVersion !== 4) throw new Error('Open legacy matches from the Play page.');
    match = found; privateView = false; matchPreview = null; connect(match.matchId);
    if (match.status === 'finished') await loadFinal();
  }); }
  async function loadFinal() { if (match) {snapshot = await request<Snapshot>(`/api/v4/matches/${match.matchId}/final`);replayEnd=snapshot.tick;replayTick=snapshot.tick;} }
 async function seekReplay(){if(!match)return;const sequence=++seekSequence;const page=Math.floor(replayTick/100);try{const result=await request<{frames:Snapshot[]}>(`/api/v4/matches/${match.matchId}/replay?page=${page}`);if(sequence!==seekSequence)return;if(!result.frames.length)throw new Error('No replay frames are available for this tick.');const nearest=result.frames.reduce((best,frame)=>Math.abs(frame.tick-replayTick)<Math.abs(best.tick-replayTick)?frame:best,result.frames[0]);const withLayout=result.frames.filter(frame=>frame.obstacles&&frame.tick<=nearest.tick);const layout=withLayout[withLayout.length-1]??result.frames[0];snapshot={obstacles:layout.obstacles,hazards:layout.hazards,transit:layout.transit,sites:layout.sites,...nearest};}catch(e){error=e instanceof Error?e.message:String(e);}}
 async function inspectTrace(){await task(async()=>{if(!match)return;trace=JSON.stringify(await request(`/api/v4/matches/${match.matchId}/trace?tick=${replayTick}`),null,2);});}
  async function register() { await task(async () => { if (!match) return; if (dirty) throw new Error('Save your browser changes before registering. Matches run the saved workspace file.'); const before = new Set(match.robots.map(r => r.robotId)); const response = await request<{ match: Match }>(`/api/matches/${match.matchId}/robots`, { method: 'POST', body: JSON.stringify({ displayName: name, team: squad, runtime: 'lua5.4', startCommand: 'lua main.lua', sdkVersion: '0.4.0', loadout: loadout() }) }); match = response.match; registeredId = match.robots.find(r => !before.has(r.robotId))?.robotId ?? ''; await refreshBox(); message = match.mode === 'arena' ? 'You are in. Your robot drops into the arena as soon as its program connects.' : 'Script and build registered. Start once all human agents are connected.'; }); }
  async function start() { await task(async () => { if (match) match = await api.startMatch(match.matchId); }); }
  async function control(command: string) { await task(async () => { if (!match) return; const result = await request<{ paused: boolean }>(`/api/v4/matches/${match.matchId}/control`, { method: 'POST', body: JSON.stringify({ command }) }); paused = result.paused; }); }
  async function readSource() { if (dirty && !confirmReload) { confirmReload = true; return; } confirmReload = false; await task(async () => { const result = await request<{ source: string; revision: string }>('/api/v4/me/script'); source = result.source; savedSource = result.source; revision = result.revision; editorLoaded = true; syntaxResult = null; }); }
  async function saveSource() { await task(async () => { const result = await request<{ source: string; revision: string }>('/api/v4/me/script', { method: 'PUT', body: JSON.stringify({ source, revision }) }); source = result.source; savedSource = result.source; revision = result.revision; message = 'Saved workspace source and a script version.'; }); }
  async function validateSource() {
    await task(async () => {
      try {
        await request('/api/v4/me/script/validate', { method: 'POST', body: JSON.stringify({ source }) });
        syntaxResult = { ok: true, message: 'Syntax passed. Run a sandbox to check behavior.' };
      } catch (e) {
        syntaxResult = { ok: false, message: e instanceof Error ? e.message : String(e) };
        throw e;
      }
    });
  }
  async function saveBuild() { await task(async () => { await request('/api/v4/me/loadout', { method: 'PUT', body: JSON.stringify(loadout()) }); message = 'Build saved to your account.'; }); }
  async function loadTemplates() { try { const result = await request<{scripts:{name:string;description:string}[]}>('/api/scripts'); templates = result.scripts; } catch { templates = []; } }
  async function deployTemplate(template: string) { await task(async () => { await api.writeBoxMain(template); const result = await request<{source:string;revision:string}>("/api/v4/me/script"); source=result.source;savedSource=result.source;revision=result.revision;editorLoaded=true;syntaxResult=null; message = `Deployed ${template}. Review, save a version, then register.`; }); }
  async function mapEdit(apply: boolean) { await task(async () => { if (!match) return; const raw = JSON.parse(edit); const result = await request(`/api/v4/matches/${match.matchId}/edit?apply=${apply}`, { method: 'POST', body: JSON.stringify(raw) }); preview = JSON.stringify(result, null, 2); message = apply ? 'Edit scheduled. Its revision applies at the effective tick.' : 'Preview validated; inspect the operations before applying.'; }); }
  async function refreshPrivate() { if (!privateView || !match || match.status !== 'running') return; const viewing=match.matchId; try { const obs = await request<{ tick: number; self: RobotState; robots: RobotState[]; obstacles: Snapshot['obstacles']; projectiles: Snapshot['projectiles']; items: { itemId: string; x: number; y: number }[]; zone: Snapshot['zone']; transit: Snapshot['transit']; sites?: Snapshot['sites']; hazards?: Snapshot['hazards']; debug?: Snapshot['debug']; revision: number; arenaWidth: number; arenaHeight: number }>(`/api/v4/matches/${match.matchId}/view`); if (!privateView || match?.matchId !== viewing) return; const robot = { ...obs.self, name: obs.self.name ?? name }; selected = robot.robotId; snapshot = { type: 'snapshot', version: 4, matchId: match.matchId, tick: obs.tick, tickRate: 20, sequence: obs.tick, status: 'running', robots: [robot, ...obs.robots.map(r => ({ ...r, name: r.name ?? r.robotId.slice(0, 8) }))], projectiles: obs.projectiles, obstacles: obs.obstacles, items: obs.items.map(i => ({ ...i, type: 'container', active: true, spawnTick: 0, pickupRadius: 35 })), zone: obs.zone, transit: obs.transit, sites: obs.sites, hazards: obs.hazards, debug: obs.debug, revision: obs.revision, width: obs.arenaWidth, height: obs.arenaHeight }; } catch (e) { privateView = false; error = e instanceof Error ? e.message : String(e); } }
  $effect(() => { panel = initialPanel; });
  // Open the workspace file automatically once the runtime is ready, so the
  // Code tab shows the editor instead of a load button.
  let autoLoadedFor = '';
  $effect(() => {
    const id = signedIn && box?.status === 'running' && panel === 'code' && !editorLoaded ? box.boxId : '';
    if (!id || id === autoLoadedFor) return;
    autoLoadedFor = id;
    untrack(() => void readSource());
  });
  // Keep the open match's row current as its status changes over the socket;
  // a periodic refresh catches other lobbies starting or finishing.
  $effect(() => {
    const current = match;
    if (!current || current.engineVersion !== 4) return;
    untrack(() => {
      const index = recentMatches.findIndex(item => item.matchId === current.matchId);
      if (index < 0) recentMatches = [current, ...recentMatches];
      else if (recentMatches[index].status !== current.status) recentMatches = recentMatches.map((item, i) => i === index ? current : item);
    });
  });
  $effect(() => { const timer = setInterval(() => { if (!matchesLoading && document.visibilityState === 'visible') void refreshMatches(); }, 15000); return () => clearInterval(timer); });
  $effect(() => { source; savedSource; revision; draftKey; editorLoaded; untrack(persistDraft); });
  $effect(() => { if (signedIn) untrack(() => void refreshBox()); else untrack(() => { persistDraft(); box = null; source = ''; savedSource = ''; revision = ''; editorLoaded = false; privateView = false; }); });
  $effect(() => { const requested = matchId; untrack(() => { if (requested && requested !== match?.matchId) { idInput=requested;void open(); } }); });
  $effect(() => { mode; capacity; size; duration; seed; siteCount; coverPerSite; lootPerSite; liveEdit; signedIn; untrack(() => schedulePreview()); });
  onMount(() => { disposed=false; void loadTemplates(); void refreshMatches(); const resume=()=>{if(document.visibilityState==='visible' && match && match.status!=='finished')connect(match.matchId);}; document.addEventListener('visibilitychange',resume); if (signedIn) void request<Loadout>('/api/v4/me/loadout').then(l => { chassis = l.chassis; weapon = l.weapon; modules = l.modules; utilities = l.utilities; }).catch(() => {}); let stopped = false; const poll = async () => { await refreshPrivate(); if (!stopped) timer = setTimeout(poll, 100); }; let timer = setTimeout(poll, 100); return () => { stopped = true; disposed=true; document.removeEventListener("visibilitychange",resume); persistDraft(); clearTimeout(timer); clearTimeout(retryTimer); clearTimeout(previewTimer); const current=socket; socket=null; current?.close(); }; });
</script>
<svelte:window onbeforeunload={beforeUnload}/>
<section class="v2">
  <header class="workspace-header" hidden={watching}>
    <div><p class="eyebrow">ROBOT WORKSPACE <span>/ ARENA V2</span></p><h1>Your code. In the arena.</h1><p class="lede">Write Lua, test a strategy, and follow every decision.</p></div>
    <div class="workspace-meta"><span class="runtime-badge"><i class:ready={box?.status === 'running'}></i>{!signedIn ? 'Guest workspace' : boxLoading ? 'Checking runtime' : box?.status === 'running' ? 'Runtime online' : 'Runtime setup needed'}</span><a href="#/docs/sdk">SDK reference ↗</a></div>
  </header>
  <nav class="workflow" aria-label="Robot workflow">
    {#each stages as stage, index}<a href={stage.href} class:active={panel === stage.id} aria-current={panel === stage.id ? 'page' : undefined}><span>{String(index + 1).padStart(2, '0')}</span>{stage.label}{#if stage.id === 'code' && dirty}<i aria-label="Unsaved changes"></i>{/if}</a>{/each}
  </nav>
  {#if welcome && match?.matchId === welcome}
    <div class="welcome-banner" role="status">
      <div><strong>Your robot is in the arena!</strong><p>The camera follows it and the panel on the right shows its health and weapon. Click any other robot to inspect it. When the match ends you can replay it from Results, and open <a href="#/workspace">My robot</a> to change how it thinks.</p></div>
      <button aria-label="Dismiss" onclick={() => welcome = ''}>×</button>
    </div>
  {/if}
  {#if error}<div class="notice error" role="alert"><span>{error}</span><button aria-label="Dismiss error" onclick={() => error = ''}>×</button></div>{/if}
  {#if message}<div class="notice" role="status"><span>{message}</span><button aria-label="Dismiss message" onclick={() => message = ''}>×</button></div>{/if}
  {#if signedIn && !boxLoading && (!box || box.status !== 'running')}
    <div class="setup-note"><div><strong>Your robot box is starting</strong><p>It is created automatically when you sign in and usually takes a few seconds. You can also play straight away from the Play page.</p></div><a class="button-link" href="#/box">Set up runtime →</a><button class="quiet" onclick={refreshBox} disabled={boxLoading}>Refresh</button></div>
  {/if}
  <div class="workspace-v2" class:editing={panel === 'code'} class:watching>
    <aside aria-label={panel === 'code' ? 'Code editor' : 'Workspace controls'}>
      <div hidden={panel !== 'code'}>
        {#if confirmReload}<div class="notice caution"><div><strong>Replace your unsaved draft?</strong><p>Reload reads the file from your runtime. Your browser changes will be discarded.</p><div class="actions"><button onclick={readSource} disabled={busy}>Discard draft and reload</button><button class="quiet" onclick={() => confirmReload = false}>Keep editing</button></div></div></div>{/if}
        <WorkspaceEditor bind:source {signedIn} {editorLoaded} {busy} {dirty} {templates} {syntaxResult} onload={readSource} onsave={saveSource} onvalidate={validateSource} ondeploy={deployTemplate} {onSignIn} oninput={() => syntaxResult = null}/>
        <div class="editor-next"><span>{dirty ? 'Save your changes before testing.' : 'Ready to see how your robot behaves?'}</span><button class="primary" onclick={prepareSandbox}>Test in sandbox →</button></div>
      </div>
      {#if panel === 'build'}
        <section class="surface controls"><p class="eyebrow">ROBOT CONFIGURATION</p><h2>Starting loadout</h2><p class="hint">Pick a chassis, weapon, modules, and utilities within the 60-point budget. The build is saved to your account and used when you register.</p><button class="full" onclick={() => showPanel('code')}>Back to your code →</button></section>
      {:else if panel === 'match' || panel === 'debug'}
        <section class="surface controls"><p class="eyebrow">{panel === 'debug' ? 'TEST ENVIRONMENT' : 'NEW MATCH'}</p><h2>{match ? 'Current arena open' : panel === 'debug' ? 'Set up a sandbox' : 'Set up a match'}</h2>
          <p class="hint">{match ? 'Close the current arena to configure a new one.' : 'Pick a mode, size, and map in the setup panel, then create the lobby.'}</p>
          {#if match}<button class="primary full" onclick={closeMatch}>New match setup</button>{/if}
        </section>
        {#if panel === 'debug'}
          <details class="surface controls advanced"><summary>Advanced map editing</summary><p class="hint">Requires an admin account and a match with live editing enabled. Use a future tick and the current revision.</p><textarea class="code" aria-label="Map edit JSON" bind:value={edit} spellcheck="false"></textarea><div class="actions"><button onclick={() => mapEdit(false)} disabled={busy || !match}>Preview</button><button onclick={() => mapEdit(true)} disabled={busy || !preview || !match}>Schedule edit</button></div>{#if preview}<pre>{preview}</pre>{/if}</details>
        {/if}
      {:else if panel === 'results'}
        <section class="surface controls"><p class="eyebrow">MATCH REVIEW</p><h2>Learn from your last run</h2><p class="hint">Open a finished match to scrub its replay. Sign in to inspect your robot's recorded decisions.</p><button class="full" onclick={() => showPanel('code')}>Back to your code →</button></section>
      {/if}
      {#if panel !== 'code'}
        <section class="surface controls recent"><div class="section-heading"><h2>{panel === 'results' ? 'Recent results' : 'Recent arenas'}</h2><button class="quiet" onclick={refreshMatches} disabled={matchesLoading}>{matchesLoading ? 'Loading…' : 'Refresh'}</button></div>
          {#if matchesError}<p class="validation-error">{matchesError}</p>{:else if matchesLoading && !recentMatches.length}<p class="hint">Loading arenas…</p>{:else if !recentMatches.filter(item => (panel !== 'results' || item.status === 'finished') && !(item.mode === 'arena' && item.status === 'failed')).length}<p class="hint">{panel === 'results' ? 'No finished Arena V2 matches yet. Complete a sandbox or match to start reviewing.' : 'No Arena V2 matches yet. Create the first lobby above.'}</p>{:else}
            {#each recentMatches.filter(item => (panel !== 'results' || item.status === 'finished') && !(item.mode === 'arena' && item.status === 'failed')).slice(0, 8) as item}<button class="match-row" class:selected={match?.matchId === item.matchId} onclick={() => openListed(item)} disabled={busy}><span><strong>{matchTitle(item)}</strong><small>{item.matchId.slice(0, 8)} · {new Date(item.createdAt).toLocaleDateString()}</small></span><span class="state-label">{item.status}</span></button>{/each}
          {/if}
        </section>
      {/if}
    </aside>
    <main>
      <section class="surface arena-panel">
        {#if panel !== 'build' && (match || panel !== 'results')}
        <div class="matchbar"><div><p class="eyebrow">{match ? 'CURRENT ARENA' : 'RUN & OBSERVE'}</p><h2><span class="status-dot" class:live={match?.status === 'running'}></span>{match ? matchTitle(match) : 'Your next run starts here'}</h2>{#if match}<a class="match-id" href={`#/v2/${match.matchId}`}>{match.matchId}</a>{/if}</div>{#if match}<span class="state-label">{match.status}</span>{/if}</div>
        {/if}
        {#if panel === 'build'}
          <div class="setup-wrap"><LoadoutBuilder bind:chassis bind:weapon bind:modules bind:utilities {busy} {signedIn} onsave={saveBuild}/></div>
        {:else if !match && (panel === 'match' || panel === 'debug')}
          <div class="setup-wrap"><MatchSetup bind:mode bind:teamSize bind:botFill bind:botCount bind:capacity bind:size bind:duration bind:siteCount bind:coverPerSite bind:lootPerSite bind:seed bind:liveEdit {signedIn} {busy} preview={mapPreview} {previewLoading} testing={panel === 'debug'} onmodechange={modeDefaults} oncreate={create} onrandomize={randomizeSeed} {onSignIn}/></div>
        {:else if !match && panel === 'results'}
          <div class="setup-wrap grid gap-3 grid-cols-1">
            <p class="m-0 text-sm text-muted-foreground">Pick a finished match to scrub its replay, click robots to inspect them, and check your robot's recorded decisions.</p>
            <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 grid-cols-1">
              {#each recentMatches.filter(item => item.status === 'finished').slice(0, 12) as item (item.matchId)}
                <a class="grid gap-1 rounded-xl border border-border bg-background p-4 text-foreground no-underline transition-colors hover:border-primary/60 hover:bg-muted/40 grid-cols-1" href={`#/v2/${item.matchId}`}>
                  <span class="flex items-center justify-between gap-2"><strong class="text-sm">{matchTitle(item)}</strong><span class="font-mono text-[11px] text-muted-foreground">{item.matchId.slice(0, 8)}</span></span>
                  <span class="text-xs text-muted-foreground">{new Date(item.createdAt).toLocaleString()}</span>
                  <span class="text-sm">{item.winnerTeam ? `Winner: ${winnerName(item)}` : 'Finished'}</span>
                  <span class="text-xs text-primary">Open replay →</span>
                </a>
              {:else}
                <p class="m-0 text-sm text-muted-foreground">{matchesLoading ? 'Loading results…' : 'No finished matches yet. Run a sandbox to create your first replay.'}</p>
              {/each}
            </div>
          </div>
        {:else if !match}
          <div class="empty-arena"><div class="arena-mark" aria-hidden="true"><span>lua</span><span>→</span><span>arena</span></div><h2>Turn a script into a competitor.</h2><p>Keep your code and tools together. Run a small sandbox, inspect what happened, then bring the same robot into a match.</p>
            <ol class="run-steps"><li class:done={editorLoaded && !dirty}><span>01</span><div><strong>Prepare your script</strong><p>Load main.lua or deploy a strategy, then save your changes.</p></div></li><li><span>02</span><div><strong>Test and debug</strong><p>Create a sandbox, register your robot, and start the simulation.</p></div></li><li><span>03</span><div><strong>Compete and review</strong><p>Choose a mode. Replay a finished match to improve your next run.</p></div></li></ol>
            <div class="actions">{#if !signedIn}<button class="primary" onclick={onSignIn}>Sign in to start coding</button><a href="#/workspace/matches">Browse arenas →</a>{:else}<button class="primary" onclick={prepareSandbox}>Set up a sandbox →</button><a href="#/docs/sdk">Read the Lua SDK</a>{/if}</div>
          </div>
        {:else}
          {#if match.status === 'failed'}<p class="notice error" role="alert">{match.error || 'This match failed. Create a new lobby to try again.'}</p>{/if}
          {#if match.status === 'lobby'}
            <div class="lobby-steps grid grid-cols-1 gap-4">
              <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div class="rounded-lg bg-muted/60 p-3"><span class="block text-xs text-muted-foreground">Players joined</span><strong class="font-mono text-xl">{match.robots.length}<span class="text-sm text-muted-foreground"> / {lobbyBots === undefined ? lobbyCapacity : lobbyCapacity - lobbyBots}</span></strong></div>
                <div class="rounded-lg bg-muted/60 p-3"><span class="block text-xs text-muted-foreground">Bots at start</span><strong class="font-mono text-xl">{lobbyBots === undefined ? 'fill' : lobbyBots}</strong><span class="block text-xs text-muted-foreground">{lobbyBots === undefined ? 'every empty slot' : 'fixed count'}</span></div>
                <div class="rounded-lg bg-muted/60 p-3"><span class="block text-xs text-muted-foreground">Mode</span><strong class="text-sm">{matchTitle(match)}</strong></div>
              </div>
              <div class="grid grid-cols-1 gap-2 rounded-lg border border-border p-3">
                <span class="text-sm font-medium">Invite friends</span>
                <div class="flex flex-wrap items-center gap-2"><code class="min-w-0 flex-1 truncate rounded-md bg-background px-3 py-2 font-mono text-xs">{location.origin}/#/v2/{match.matchId}</code><button onclick={copyLink}>{copiedLink ? 'Copied ✓' : 'Copy link'}</button></div>
                <span class="text-xs text-muted-foreground">Anyone signed in can open the link, register their robot, and join. Logged-out visitors can watch.</span>
              </div>
              <div class="registration"><div class="robot-fields"><label>Robot name<input bind:value={name} maxlength="32"/></label>{#if match.mode === 'br-squad'}<label>Team<select bind:value={squad}><option value="">Any open team</option>{#each lobbyTeams as team}<option value={team}>{team}</option>{/each}<option value={`team-${lobbyTeams.length + 1}`}>New team</option></select></label>{/if}</div><p class="hint">{title(chassis)} · {title(weapon)} · {cost}/60 points <a href="#/workspace/build">Edit loadout</a></p>
                {#if dirty}<p class="validation-error">Save your browser changes before registering. The match uses the file saved in your runtime.</p>{/if}
                <div class="actions"><button class="primary" onclick={register} disabled={busy || !canRegister || registered}>{registered ? 'Robot registered' : 'Join with my robot'}</button><button onclick={start} disabled={busy || !signedIn}>Start match</button></div><p class="hint">Joining snapshots your saved main.lua. The lobby owner starts the match once everyone's robot is connected.</p>
              </div>
            </div>
            {#if matchPreview}<MapPreview preview={matchPreview}/>{/if}
          {:else if match.status === 'queued'}<p class="notice" role="status">Your match is queued. The simulation will appear when the worker starts it.</p>{/if}
          {#if match.mode === 'arena' && match.status === 'running' && !registered}
            <div class="arena-join">
              <div><strong>The Arena is live.</strong> <span class="hint">It never ends: drop in any time, stay inside the drifting safe zone, respawn when destroyed, leave whenever you like. Your robot runs your saved main.lua with your saved build.</span></div>
              {#if signedIn}
                <label class="arena-name">Robot name<input bind:value={name} maxlength="32"/></label>
                <button class="primary" onclick={register} disabled={busy || !canRegister}>{busy ? 'Joining…' : 'Join the Arena ▶'}</button>
                {#if box?.status !== 'running'}<span class="hint">Your robot box is starting…</span>{/if}
              {:else}<span class="hint">Sign in to play. Anyone can watch.</span>{/if}
            </div>
          {/if}
          {#if myRobotId && (match.status === 'lobby' || match.status === 'queued' || match.status === 'running')}
            <div class="leave-bar">
              <span><i class="status-dot live"></i>You are playing as <strong>{match.robots.find(r => r.robotId === myRobotId)?.displayName ?? 'your robot'}</strong></span>
              {#if confirmLeave}
                <span class="leave-confirm">Leave? Your robot is removed and your box is freed for another match.</span>
                <button class="danger" onclick={leaveMatch} disabled={busy}>{busy ? 'Leaving…' : 'Leave match'}</button><button class="quiet" onclick={() => confirmLeave = false}>Stay</button>
              {:else}
                <button class="quiet" onclick={() => confirmLeave = true}>Leave match</button>
              {/if}
            </div>
          {/if}
          {#if match.mode === 'sandbox' && match.status === 'running'}<div class="sandbox-toolbar"><span class="state-label">{paused ? 'Paused' : 'Running'}</span><button onclick={() => control(paused ? 'resume' : 'pause')} disabled={busy || !signedIn}>{paused ? 'Resume' : 'Pause'}</button><button onclick={() => control('step')} disabled={busy || !signedIn}>Step one tick</button><span class="hint">Owner controls</span></div>{/if}
          {#if match.status === 'finished' && match.mode === 'arena'}
            {@const podium = [...(match.robotSummaries ?? [])].sort((a, b) => (b.score ?? 0) - (a.score ?? 0)).slice(0, 3)}
            <div class="arena-over">
              <div><p class="eyebrow">SESSION OVER</p><h2>Top of this Arena session</h2></div>
              <ol>{#each podium as p, i}<li><span>{['🥇', '🥈', '🥉'][i]}</span><strong>{p.name}</strong><em>{p.score ?? 0} pts · {p.kills} K · {p.deaths ?? 0} D</em>{#if p.playerId}<a href={`#/profile/${p.playerId}`}>profile</a>{/if}</li>{/each}</ol>
              <div class="actions"><button class="primary" onclick={nextArena} disabled={busy}>{nextArenaWaiting ? 'Finding the next session…' : 'Go to the next session →'}</button><a href="#/leaderboard">Leaderboard</a></div>
            </div>
          {:else if match.status === 'finished'}<div class="result-heading"><div><p class="eyebrow">MATCH COMPLETE</p><h2>{match.winnerTeam ? `${winnerName(match)} wins` : 'Final results'}</h2></div><button onclick={() => showPanel('code')}>Revise your script →</button></div>{/if}
          {#if replayEnd > 0}<div class="replay"><label>Replay <strong>{(replayTick / (snapshot?.tickRate ?? 20)).toFixed(1)}s / {(replayEnd / (snapshot?.tickRate ?? 20)).toFixed(1)}s</strong><input aria-label="Replay tick" type="range" min="0" max={replayEnd} step="10" bind:value={replayTick} onchange={seekReplay}/></label><button onclick={inspectTrace} disabled={busy || !signedIn}>Inspect my decision at this tick</button>{#if trace}<pre>{trace}</pre>{/if}</div>{/if}
          {#if snapshot || match.status === 'running' || match.status === 'finished'}
            <div class="world-split">
              <div class="world-main">
                <div class="map-stage">{#key replayEnd > 0 ? snapshot?.tick : `${match.matchId}:${privateView}:${viewerEpoch}`}<WorldView {snapshot} {selected} {leaderId} youId={myRobotId} sites={matchPreview?.sites ?? []} overview={privateView || replayEnd > 0 ? [] : overview} onregion={cameraRegion} {picked} onselect={id => { selected = id; picked = null; }} onpick={thing => { picked = thing; selected = ''; }}/>{/key}{#if replayEnd === 0}<KillFeed events={feed} robots={overview.length ? overview : snapshot?.robots ?? []}/>{/if}</div>
                {#if match.status !== 'finished'}<div class="view-controls"><label class="check"><input type="checkbox" bind:checked={privateView} disabled={!signedIn || !registered} onchange={() => snapshot = null}/>My robot's live view</label><span>{connectionStatus} · {privateView ? 'Robot observations' : 'Public view delayed 5s'}</span></div>{/if}
                {#if match.status === 'running' && !snapshot}<p class="hint loading-snapshot">Waiting for a snapshot. Public spectating starts after five seconds of simulation.</p>{/if}
                <MatchOverview {snapshot} roster={privateView || replayEnd > 0 ? [] : overview} youId={privateView ? selected : ''}/>
              </div>
              <aside class="world-side" aria-label="Robot details">
                {#if picked && !selected}<ThingInspector {picked} sites={snapshot?.sites ?? matchPreview?.sites ?? []} onclose={() => picked = null}/>{:else}<RobotInspector robot={inspected} tick={snapshot?.tick ?? 0} tickRate={snapshot?.tickRate ?? 20} youId={privateView ? selected : ''} {leaderId} onclose={() => selected = ''} onfollow={id => { selected = ''; queueMicrotask(() => selected = id); }}/>{/if}
                {#if match.mode === 'arena'}<ArenaScoreboard robots={overview.length ? overview : snapshot?.robots ?? []} youId={myRobotId} endTick={snapshot?.zone?.next?.arrivesAt ?? 0} tick={snapshot?.tick ?? 0} tickRate={snapshot?.tickRate ?? 20} onselect={id => selected = id}/>{/if}
                <VirtualRoster robots={privateView || replayEnd > 0 ? snapshot?.robots ?? [] : overview} {selected} youId={privateView ? selected : ''} onselect={id => selected = id}/>
              </aside>
            </div>
          {/if}
        {/if}
      </section>
      <form class="surface join" onsubmit={event => { event.preventDefault(); void open(); }}><label>Open a match by ID<input aria-label="Match ID" bind:value={idInput} placeholder="Paste a match ID"/></label><button type="submit" disabled={busy || !idInput.trim()}>Open match →</button></form>
      <div class="workspace-footnote"><span>Lua 5.4 · SDK 0.4</span><span>Local workspace · Unranked matches</span></div>
    </main>
  </div>
</section>
<style>
  .v2 .setup-wrap { padding:20px 22px; }
  .v2 .arena-join { display:flex; flex-wrap:wrap; align-items:end; gap:12px; padding:14px 20px; border-bottom:1px solid var(--v2-border); background:color-mix(in srgb, var(--color-primary) 8%, transparent); }
  .v2 .arena-join > div { flex:1 1 280px; }
  .v2 .arena-name { display:grid; gap:4px; font-size:12px; }
  .v2 .leave-bar { display:flex; flex-wrap:wrap; align-items:center; gap:10px; padding:12px 20px; border-bottom:1px solid var(--v2-border); font-size:13px; }
  .v2 .leave-bar > span:first-child { display:flex; align-items:center; gap:8px; margin-right:auto; }
  .v2 .leave-confirm { color:#f2a49b; font-size:12px; }
  .v2 .leave-bar .danger { border-color:#7a3a36; color:#ffb4ab; background:#2a1414; }
  .v2 .welcome-banner { display:flex; gap:16px; align-items:flex-start; padding:16px 18px; margin-bottom:20px; border:1px solid color-mix(in srgb, var(--v2-accent) 50%, transparent); border-radius:12px; background:color-mix(in srgb, var(--v2-accent) 10%, transparent); animation:welcome-in .5s ease both; }
  .v2 .welcome-banner strong { font-size:15px; }
  .v2 .welcome-banner p { margin:4px 0 0; font-size:13px; color:var(--v2-muted); line-height:1.6; }
  .v2 .welcome-banner button { margin-left:auto; background:none; border:0; color:var(--v2-muted); font-size:20px; cursor:pointer; }
  @keyframes welcome-in { from { opacity:0; transform:translateY(-6px); } }
  @media (max-width:560px) { .v2 .setup-wrap { padding:12px 0; } .v2 .world-split { padding:0 0 12px; } }
  .v2 .world-split { display:grid; gap:16px; padding:0 22px 16px; grid-template-columns:minmax(0,1fr); }
  .v2 .world-main, .v2 .world-side { display:grid; gap:14px; align-content:start; min-width:0; }
  .v2 .workspace-v2.watching { grid-template-columns:minmax(0,1fr); }
  .v2 .map-stage { position:relative; min-width:0; }
  .v2 .arena-over { display:grid; gap:12px; padding:18px 22px; border-bottom:1px solid var(--v2-border); background:color-mix(in srgb, var(--color-primary) 8%, transparent); }
  .v2 .arena-over ol { margin:0; padding:0; display:grid; gap:6px; }
  .v2 .arena-over li { list-style:none; display:flex; align-items:baseline; gap:10px; flex-wrap:wrap; }
  .v2 .arena-over li em { font-style:normal; color:var(--v2-muted); font:12px 'DM Mono',monospace; }
  .v2 .arena-over .actions { display:flex; gap:12px; align-items:center; flex-wrap:wrap; }
  .v2 .workspace-v2.watching > aside { display:none; }
  .v2 .workspace-v2.watching .matchbar { padding:10px 20px; }
  .v2 .workspace-v2.watching .matchbar .eyebrow { display:none; }
  .v2 .workspace-v2.watching .matchbar > div { display:flex; align-items:baseline; gap:12px; flex-wrap:wrap; }
  .v2 .workspace-v2.watching .match-id { margin:0; border:0; padding:0; background:none; }
  .v2 .workspace-v2.watching .arena-join { padding:10px 20px; align-items:center; }
  @media (min-width:1024px) { .v2 .world-split { grid-template-columns:minmax(0,1fr) 380px; align-items:start; } .v2 .world-side { position:sticky; top:12px; max-height:calc(100vh - 24px); overflow:auto; overscroll-behavior:contain; } }
  .v2 { --v2-bg:#0d141d; --v2-panel:#111c27; --v2-border:#263544; --v2-text:#e0e8ef; --v2-muted:#91a2b3; --v2-accent:#73dfc7; color:var(--v2-text); max-width:1640px; padding:32px clamp(18px,3vw,44px) 52px; margin:auto; }
  .v2 .workspace-header { display:flex; justify-content:space-between; align-items:center; gap:24px; margin-bottom:27px; }
  .v2 .eyebrow { font:500 10px/1.5 'DM Mono',monospace; letter-spacing:.13em; color:var(--v2-muted); margin:0 0 8px; }
  .v2 .eyebrow span { color:#60768c; margin-left:8px; }
  .v2 h1 { font-size:clamp(25px,3vw,36px); line-height:1.2; letter-spacing:-.035em; margin:0; font-weight:600; }
  .v2 h2 { font-size:17px; line-height:1.3; margin:0; font-weight:600; letter-spacing:-.02em; }
  .v2 .lede { font-size:13px; color:var(--v2-muted); margin:10px 0 0; }
  .v2 a { color:var(--v2-accent); text-decoration:none; }
  .v2 a:hover { text-decoration:underline; }
  .v2 .workspace-meta { display:flex; flex-direction:column; align-items:end; gap:12px; font-size:12px; }
  .v2 .runtime-badge { display:flex; align-items:center; gap:8px; border:1px solid var(--v2-border); border-radius:20px; padding:7px 11px; color:var(--v2-muted); background:var(--v2-bg); font:10px 'DM Mono',monospace; }
  .v2 .runtime-badge i,.v2 .status-dot { display:inline-block; width:7px; height:7px; border-radius:50%; background:#74879a; }
  .v2 .runtime-badge i.ready,.v2 .status-dot.live { background:var(--v2-accent); }
  .v2 .workflow { display:flex; gap:4px; overflow-x:auto; scrollbar-width:none; border-bottom:1px solid var(--v2-border); margin-bottom:24px; }
  .v2 .workflow a { display:flex; align-items:center; justify-content:center; gap:9px; padding:13px 20px 15px; border-bottom:2px solid transparent; color:var(--v2-muted); white-space:nowrap; font-size:13px; }
  .v2 .workflow a:hover { color:var(--v2-text); background:#12202b; text-decoration:none; }
  .v2 .workflow a.active { color:var(--v2-accent); border-bottom-color:var(--v2-accent); }
  .v2 .workflow a span { font:10px 'DM Mono',monospace; opacity:.65; }
  .v2 .workflow a i { width:5px; height:5px; background:#f0c274; border-radius:50%; }
  .v2 .workspace-v2 { display:grid; grid-template-columns:330px minmax(0,1fr); gap:22px; align-items:start; }
  .v2 .workspace-v2.editing { grid-template-columns:minmax(0,1.25fr) minmax(330px,.9fr); }
  .v2 aside,.v2 main { min-width:0; }
  .v2 .surface { background:var(--v2-bg); border:1px solid var(--v2-border); border-radius:9px; overflow:hidden; }
  .v2 .controls { padding:20px; margin-bottom:16px; }
  .v2 .section-heading { display:flex; justify-content:space-between; align-items:center; gap:8px; margin-bottom:16px; }
  .v2 button,.v2 .button-link { display:inline-flex; align-items:center; justify-content:center; min-height:35px; padding:8px 12px; background:#172531; border:1px solid #34495c; border-radius:5px; color:var(--v2-text); font:500 12px/1.4 inherit; cursor:pointer; transition:background .15s,border-color .15s; }
  .v2 button:hover:not(:disabled),.v2 .button-link:hover { border-color:var(--v2-accent); background:#1c303a; text-decoration:none; }
  .v2 button:disabled { opacity:.4; cursor:not-allowed; }
  .v2 button.primary { background:var(--v2-accent); color:#102921; border-color:var(--v2-accent); font-weight:600; }
  .v2 button.primary:hover:not(:disabled) { background:#9defdc; }
  .v2 button.quiet { background:transparent; border-color:transparent; color:var(--v2-muted); }
  .v2 .full { width:100%; }
  .v2 :is(button,a,input,textarea,summary):focus-visible { outline:2px solid var(--v2-accent); outline-offset:3px; }
  .v2 label { display:block; font-size:12px; line-height:1.5; color:#b7c5d2; margin:17px 0; }
  .v2 label > strong { float:right; color:var(--v2-text); font-weight:500; }
  .v2 input:not([type=checkbox]):not([type=range]),.v2 textarea { width:100%; min-width:0; background:#090f17; border:1px solid #304354; border-radius:5px; padding:10px; color:var(--v2-text); box-sizing:border-box; margin:7px 0 0; font:12px/1.5 'DM Mono',monospace; }
  .v2 input[type=range] { display:block; width:100%; margin:12px 0; accent-color:var(--v2-accent); }
  .v2 input[type=checkbox] { width:15px; height:15px; margin:0; accent-color:var(--v2-accent); flex-shrink:0; }
  .v2 .check { display:flex; align-items:center; gap:9px; margin:12px 0; }
  .v2 .hint { font:12px/1.65 inherit; color:var(--v2-muted); overflow-wrap:anywhere; margin:10px 0; }
  .v2 .actions { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
  .v2 .actions a { font-size:12px; padding:8px 0; }
  .v2 .validation-error { color:#f2a49b; }
  .v2 .validation-error { font-size:12px; line-height:1.6; }
  .v2 .notice { display:flex; align-items:start; justify-content:space-between; gap:12px; background:#122d2a; color:#bfe8df; border:1px solid #28544c; border-radius:6px; padding:12px 15px; margin:0 0 18px; font-size:12px; line-height:1.6; }
  .v2 .notice button { background:transparent; border-color:transparent; color:inherit; min-height:24px; padding:0 6px; }
  .v2 .notice.error { background:#302125; border-color:#68404a; color:#ffcbc4; }
  .v2 .notice.caution { background:#302a1d; border-color:#685838; color:#eddaa8; }
  .v2 .setup-note { display:flex; align-items:center; gap:16px; padding:15px 18px; background:var(--v2-panel); border:1px solid var(--v2-border); border-radius:7px; margin-bottom:20px; font-size:12px; }
  .v2 .setup-note > div { flex:1; }
  .v2 .setup-note p { margin:4px 0 0; color:var(--v2-muted); line-height:1.6; }
  .v2 .setup-note a { white-space:nowrap; }
  .v2 .editor-next { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:12px; padding:15px 0; color:var(--v2-muted); font-size:12px; }
  .v2 .matchbar { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:20px 22px; border-bottom:1px solid var(--v2-border); }
  .v2 .matchbar h2 { display:flex; align-items:center; gap:9px; }
  .v2 .match-id { display:block; margin:10px 0 0; color:var(--v2-muted); font:10px 'DM Mono',monospace; overflow-wrap:anywhere; }
  .v2 .state-label { font:10px 'DM Mono',monospace; color:var(--v2-accent); border:1px solid #2d4947; border-radius:4px; padding:5px 8px; text-transform:capitalize; white-space:nowrap; }
  .v2 .empty-arena { padding:clamp(24px,3vw,40px); }
  .v2 .arena-mark { display:flex; align-items:center; gap:18px; width:fit-content; padding:10px 15px; border:1px solid #314957; border-radius:5px; color:var(--v2-accent); font:12px 'DM Mono',monospace; margin-bottom:24px; }
  .v2 .arena-mark span:nth-child(2) { color:#5c758a; }
  .v2 .empty-arena h2 { font-size:24px; letter-spacing:-.025em; max-width:300px; }
  .v2 .empty-arena > p { color:var(--v2-muted); font-size:13px; line-height:1.75; margin:14px 0 26px; max-width:420px; }
  .v2 .run-steps { list-style:none; padding:0; margin:0 0 28px; }
  .v2 .run-steps li { display:flex; gap:14px; padding:15px 0; border-top:1px solid var(--v2-border); }
  .v2 .run-steps li > span { color:#758ca0; font:11px/1.7 'DM Mono',monospace; }
  .v2 .run-steps li.done > span { color:var(--v2-accent); }
  .v2 .run-steps strong { font-size:12px; font-weight:500; }
  .v2 .run-steps p { font-size:12px; line-height:1.6; color:var(--v2-muted); margin:5px 0 0; }
  .v2 .join { display:flex; align-items:end; gap:12px; padding:18px 20px; margin-top:16px; }
  .v2 .join label { flex:1; min-width:0; margin:0; }
  .v2 .join button { white-space:nowrap; height:40px; }
  .v2 .workspace-footnote { display:flex; justify-content:space-between; flex-wrap:wrap; gap:8px; font:9px 'DM Mono',monospace; color:#70869a; margin-top:14px; }
  .v2 .advanced { margin:20px 0; }
  .v2 .advanced summary { color:#b7c5d2; font-size:12px; cursor:pointer; padding:3px 0; }
  .v2 .recent h2 { font-size:13px; }
  .v2 .recent .section-heading { margin-bottom:8px; }
  .v2 .match-row { display:flex; align-items:center; justify-content:space-between; gap:10px; width:100%; text-align:left; margin-top:6px; padding:12px 9px; background:transparent; border:1px solid transparent; }
  .v2 .match-row.selected { border-color:#30584e; background:#142720; }
  .v2 .match-row strong { display:block; font-size:12px; font-weight:500; }
  .v2 .match-row small { display:block; color:var(--v2-muted); font:9px 'DM Mono',monospace; margin-top:5px; }
  .v2 .match-row .state-label { font-size:9px; padding:3px 5px; }
  .v2 .lobby-steps,.v2 .replay { padding:20px 22px; border-bottom:1px solid var(--v2-border); }
  .v2 .robot-fields { display:flex; gap:12px; }
  .v2 .robot-fields label { flex:1; min-width:0; margin-bottom:0; }
  .v2 .sandbox-toolbar { display:flex; align-items:center; gap:9px; padding:14px 20px; border-bottom:1px solid var(--v2-border); flex-wrap:wrap; }
  .v2 .view-controls { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:10px; padding:8px 16px; border-bottom:1px solid var(--v2-border); color:var(--v2-muted); font-size:10px; }
  .v2 .view-controls label { margin:4px 0; font-size:11px; }
  .v2 .loading-snapshot { padding:10px 20px; }
  .v2 .result-heading { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:22px; border-bottom:1px solid var(--v2-border); background:#142720; }
  .v2 .replay label { margin:0 0 12px; }
  .v2 pre { overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; max-height:280px; background:#090f17; border:1px solid var(--v2-border); padding:13px; color:#bdcdda; font:11px/1.65 'DM Mono',monospace; border-radius:5px; }
  .v2 .code { min-height:180px; resize:vertical; }
  @media (max-width:1100px) { .v2 .workspace-v2.editing { grid-template-columns:minmax(0,1fr); } .v2 .workspace-v2.editing .empty-arena { display:none; } .v2 .workflow a { padding-inline:14px; } }
  .v2 .workflow::-webkit-scrollbar { display:none; }
  /* Single column: outside the Code tab the main panel is the primary
     content, so it comes before the sidebar. */
  @media (max-width:850px) { .v2 .workspace-v2 { grid-template-columns:minmax(0,1fr); } .v2 .workspace-v2:not(.editing) > main { order:-1; } .v2 .workspace-header { align-items:start; } .v2 .workspace-meta { font-size:11px; } .v2 .workflow a { padding-inline:11px; } .v2 .setup-note { flex-wrap:wrap; } .v2 .setup-note > div { flex-basis:100%; } }
  @media (max-width:560px) { .v2 { padding-top:22px; } .v2 .workspace-header { flex-direction:column; gap:16px; } .v2 .workspace-meta { flex-direction:row; align-items:center; justify-content:space-between; width:100%; } .v2 .workflow { gap:0; } .v2 .workflow a { padding-inline:10px; font-size:12px; } .v2 .workflow a span { display:none; } .v2 .join { flex-wrap:wrap; } .v2 .join label { flex-basis:100%; } .v2 .join button { width:100%; } .v2 .result-heading { flex-wrap:wrap; } .v2 .robot-fields { flex-direction:column; gap:0; } }
</style>
