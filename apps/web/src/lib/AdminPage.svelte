<script lang="ts">
  import { onMount } from 'svelte';
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Input from './components/ui/input.svelte';
  import Select from './components/ui/select.svelte';
  import Badge from './components/ui/badge.svelte';
  import ChoiceGroup from './components/ui/choice-group.svelte';

  interface LogEntry { seq: number; time: string; level: string; source: string; matchId?: string; message: string; attrs?: Record<string, string> }
  interface Overview {
    server: { host: string; goVersion: string; uptimeSeconds: number; goroutines: number; heapMB: number; cpus: number };
    cloud: Record<string, string>; matches: Record<string, number>; agents: number; viewers: Record<string, number>; queue: number;
    workers: { matchId: string; tick: number; paused: boolean; agents: number }[];
    logs: { errors15m: number; warnings15m: number }; defaultAdminPassword: boolean; settings: Record<string, string>;
  }
  interface AdminMatch { matchId: string; mode: string; status: string; engineVersion?: number; createdAt: string; ownerId?: string; error?: string; robots: { robotId: string; displayName: string; bot?: boolean }[] }
  interface BoxSummary { boxId: string; state: string; status: string; createdAt: string }

  const STORAGE_KEY = 'robot-arena:admin-session';
  let token = $state(''), username = $state('admin'), password = $state(''), loginError = $state(''), loggingIn = $state(false);
  let tab = $state<'overview' | 'logs' | 'matches' | 'boxes'>('overview');
  let overview = $state<Overview | null>(null), matches = $state<AdminMatch[]>([]), boxes = $state<BoxSummary[]>([]);
  let error = $state('');
  let logs = $state<LogEntry[]>([]), latest = 0, paused = $state(false), follow = $state(true);
  let source = $state(''), level = $state(''), matchFilter = $state(''), search = $state('');
  let selectedBox = $state(''), boxLogs = $state(''), boxTail = $state(300), boxLoading = $state(false);
  let logPane: HTMLDivElement | undefined = $state();

  try { token = sessionStorage.getItem(STORAGE_KEY) ?? ''; } catch { /* storage unavailable: login per page load */ }

  async function call<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', 'X-Admin-Session': token, ...init?.headers } });
    const body = await response.json().catch(() => ({}));
    if (response.status === 401 && path !== '/api/admin/login') { logout(); throw new Error('Session expired. Log in again.'); }
    if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`);
    return body as T;
  }

  async function login(event: SubmitEvent) {
    event.preventDefault();
    loggingIn = true; loginError = '';
    try {
      const result = await call<{ token: string }>('/api/admin/login', { method: 'POST', body: JSON.stringify({ username, password }) });
      token = result.token; password = '';
      try { sessionStorage.setItem(STORAGE_KEY, token); } catch { /* keep in memory only */ }
      void refreshAll();
    } catch (e) { loginError = e instanceof Error ? e.message : String(e); }
    finally { loggingIn = false; }
  }
  function logout() {
    token = ''; overview = null; logs = []; latest = 0;
    try { sessionStorage.removeItem(STORAGE_KEY); } catch { /* nothing stored */ }
  }

  async function refreshOverview() { overview = await call<Overview>('/api/admin/overview'); }
  async function refreshMatches() { matches = (await call<{ matches: AdminMatch[] }>('/api/admin/matches')).matches ?? []; }
  async function refreshBoxes() { boxes = (await call<{ boxes: BoxSummary[] }>('/api/admin/boxes')).boxes ?? []; }
  async function refreshAll() {
    error = '';
    try { await Promise.all([refreshOverview(), refreshMatches(), refreshBoxes().catch(e => { error = `Boxes: ${e.message}`; })]); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
  }

  function logQuery(after: number) {
    const q = new URLSearchParams({ after: String(after), limit: '1000' });
    if (source) q.set('source', source);
    if (level) q.set('level', level);
    if (matchFilter.trim()) q.set('match', matchFilter.trim());
    if (search.trim()) q.set('q', search.trim());
    return `/api/admin/logs?${q}`;
  }
  async function pollLogs(reset = false) {
    if (!token) return;
    if (reset) { logs = []; latest = 0; }
    const result = await call<{ entries: LogEntry[]; latest: number }>(logQuery(latest));
    latest = result.latest;
    if (result.entries.length) {
      logs = [...logs, ...result.entries].slice(-3000);
      if (follow) queueMicrotask(() => logPane?.scrollTo({ top: logPane.scrollHeight }));
    }
  }
  // Filters change the query, so restart the tail from the beginning.
  $effect(() => { source; level; matchFilter; search; if (token) void pollLogs(true).catch(() => {}); });

  async function openBoxLogs(boxId: string) {
    selectedBox = boxId; boxLoading = true; boxLogs = '';
    try { boxLogs = (await call<{ logs: string }>(`/api/admin/boxes/${boxId}/logs?tail=${boxTail}`)).logs || '(no output yet)'; }
    catch (e) { boxLogs = e instanceof Error ? e.message : String(e); }
    finally { boxLoading = false; }
  }
  function workerLogs(matchId: string) { matchFilter = matchId; source = ''; tab = 'logs'; }

  onMount(() => {
    if (token) void refreshAll();
    const logTimer = setInterval(() => { if (token && !paused && tab === 'logs') void pollLogs().catch(() => {}); }, 2000);
    const overviewTimer = setInterval(() => { if (token && tab === 'overview') void refreshOverview().catch(() => {}); }, 5000);
    return () => { clearInterval(logTimer); clearInterval(overviewTimer); };
  });

  const levelTone: Record<string, 'danger' | 'warn' | 'muted' | 'default'> = { ERROR: 'danger', WARN: 'warn', INFO: 'default', DEBUG: 'muted' };
  const statusTone = (status: string) => status === 'running' ? 'live' : status === 'failed' ? 'danger' : status === 'finished' ? 'default' : 'muted';
  const uptime = (s: number) => s >= 86400 ? `${Math.floor(s / 86400)}d ${Math.floor(s % 86400 / 3600)}h` : s >= 3600 ? `${Math.floor(s / 3600)}h ${Math.floor(s % 3600 / 60)}m` : `${Math.floor(s / 60)}m ${s % 60}s`;
  const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour12: false });
  const tabs = [{ value: 'overview' as const, label: 'Overview' }, { value: 'logs' as const, label: 'Logs' }, { value: 'matches' as const, label: 'Matches' }, { value: 'boxes' as const, label: 'Boxes' }];
</script>

<section class="mx-auto grid max-w-[1400px] gap-5 px-4 py-8 sm:px-8 grid-cols-1">
  {#if !token}
    <Card class="mx-auto mt-10 w-full max-w-sm">
      <CardHeader><CardTitle>Operations console</CardTitle><CardDescription>Administrator login. This is separate from player sign-in.</CardDescription></CardHeader>
      <CardContent>
        <form class="grid gap-3 grid-cols-1" onsubmit={login}>
          <label class="m-0 font-sans normal-case tracking-normal grid gap-1.5 text-sm grid-cols-1">Username<Input bind:value={username} ariaLabel="Username"/></label>
          <label class="m-0 font-sans normal-case tracking-normal grid gap-1.5 text-sm grid-cols-1">Password<Input type="password" bind:value={password} ariaLabel="Password"/></label>
          {#if loginError}<p class="m-0 text-sm text-destructive" role="alert">{loginError}</p>{/if}
          <Button type="submit" disabled={loggingIn || !password}>{loggingIn ? 'Signing in…' : 'Sign in'}</Button>
        </form>
      </CardContent>
    </Card>
  {:else}
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div><p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">OPERATIONS</p><h1 class="m-0 text-3xl font-semibold tracking-tight">Admin console</h1></div>
      <div class="flex items-center gap-2"><Button variant="secondary" onclick={refreshAll}>Refresh</Button><Button variant="ghost" onclick={logout}>Log out</Button></div>
    </header>
    {#if overview?.defaultAdminPassword}<div class="rounded-lg border border-warning/50 bg-warning/10 px-4 py-2.5 text-sm text-warning" role="alert">The admin password is still the local default. Set <code>ADMIN_PASSWORD</code> before exposing this stack.</div>{/if}
    {#if error}<div class="rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-2.5 text-sm text-destructive" role="alert">{error}</div>{/if}
    <ChoiceGroup ariaLabel="Console section" layout="segmented" class="max-w-xl" bind:value={tab} choices={tabs}/>

    {#if tab === 'overview'}
      {#if overview}
        <div class="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6">
          {#each [
            ['Uptime', uptime(overview.server.uptimeSeconds), overview.server.host],
            ['Live workers', overview.workers.length, `${overview.matches.running ?? 0} running · ${overview.matches.queued ?? 0} queued`],
            ['Agents', overview.agents, 'connected robot sockets'],
            ['Viewers', Object.values(overview.viewers).reduce((a, b) => a + b, 0), `${Object.keys(overview.viewers).length} watched matches`],
            ['Errors · 15 min', overview.logs.errors15m, `${overview.logs.warnings15m} warnings`],
            ['Memory', `${overview.server.heapMB} MB`, `${overview.server.goroutines} goroutines · ${overview.server.cpus} CPU`],
          ] as [label, value, detail]}
            <Card><CardContent class="grid gap-1 p-4 grid-cols-1"><span class="text-xs text-muted-foreground">{label}</span><strong class="font-mono text-2xl" class:text-destructive={label === 'Errors · 15 min' && Number(value) > 0}>{value}</strong><span class="truncate text-xs text-muted-foreground">{detail}</span></CardContent></Card>
          {/each}
        </div>
        <div class="grid gap-5 lg:grid-cols-2 grid-cols-1">
          <Card>
            <CardHeader><CardTitle>Match workers</CardTitle><CardDescription>One Rust worker process per running Arena V2 match.</CardDescription></CardHeader>
            <CardContent>
              {#if overview.workers.length}
                <table class="w-full text-sm"><thead><tr class="text-left text-xs text-muted-foreground"><th class="py-1.5 font-normal">Match</th><th class="font-normal">Tick</th><th class="font-normal">Agents</th><th class="font-normal">State</th><th></th></tr></thead>
                  <tbody>{#each overview.workers as w (w.matchId)}<tr class="border-t border-border"><td class="py-2 font-mono text-xs">{w.matchId.slice(0, 8)}</td><td class="font-mono">{w.tick} <span class="text-xs text-muted-foreground">({(w.tick / 20).toFixed(0)}s)</span></td><td>{w.agents}</td><td><Badge variant={w.paused ? 'warn' : 'live'}>{w.paused ? 'paused' : 'running'}</Badge></td><td class="text-right"><Button size="sm" variant="ghost" onclick={() => workerLogs(w.matchId)}>Logs</Button></td></tr>{/each}</tbody></table>
              {:else}<p class="m-0 text-sm text-muted-foreground">No match is running.</p>{/if}
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>Platform</CardTitle></CardHeader>
            <CardContent class="grid gap-3 text-sm grid-cols-1">
              <div class="flex flex-wrap gap-1.5">{#each Object.entries(overview.matches) as [status, count]}<Badge variant={statusTone(status)}>{status} {count}</Badge>{/each}</div>
              <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
                {#each Object.entries(overview.cloud) as [key, value]}<dt class="text-muted-foreground">cloud.{key}</dt><dd class="m-0 truncate font-mono text-xs">{value}</dd>{/each}
                <dt class="text-muted-foreground">queue</dt><dd class="m-0 font-mono text-xs">{overview.queue} waiting</dd>
                <dt class="text-muted-foreground">go</dt><dd class="m-0 font-mono text-xs">{overview.server.goVersion}</dd>
                {#each Object.entries(overview.settings) as [key, value]}<dt class="text-muted-foreground">{key}</dt><dd class="m-0 truncate font-mono text-xs">{value || '—'}</dd>{/each}
              </dl>
            </CardContent>
          </Card>
        </div>
      {:else}<p class="text-sm text-muted-foreground">Loading overview…</p>{/if}

    {:else if tab === 'logs'}
      <Card class="overflow-hidden">
        <div class="flex flex-wrap items-center gap-2 border-b border-border p-3">
          <Select class="w-44" bind:value={source} ariaLabel="Source">
            <option value="">All sources</option><option value="api">API server</option><option value="worker">Match workers</option><option value="agent">Robot agents</option><option value="admin">Admin</option>
          </Select>
          <Select class="w-36" bind:value={level} ariaLabel="Minimum level">
            <option value="">All levels</option><option value="INFO">Info+</option><option value="WARN">Warnings+</option><option value="ERROR">Errors</option>
          </Select>
          <Input class="w-56" placeholder="Match ID" bind:value={matchFilter} ariaLabel="Match ID filter"/>
          <Input class="w-56" placeholder="Search text" bind:value={search} ariaLabel="Search logs"/>
          <label class="m-0 font-sans normal-case tracking-normal flex items-center gap-1.5 text-xs text-muted-foreground"><input type="checkbox" class="size-4 accent-primary" bind:checked={follow}/>Auto-scroll</label>
          <div class="ml-auto flex gap-2"><Button size="sm" variant="secondary" onclick={() => paused = !paused}>{paused ? 'Resume' : 'Pause'}</Button><Button size="sm" variant="ghost" onclick={() => { source = ''; level = ''; matchFilter = ''; search = ''; }}>Clear filters</Button></div>
        </div>
        <div bind:this={logPane} class="h-[62vh] overflow-auto bg-background font-mono text-xs leading-relaxed" role="log" aria-live="off">
          {#each logs as entry (entry.seq)}
            <div class="grid grid-cols-[76px_64px_110px_1fr] gap-2 border-b border-border/40 px-3 py-1 hover:bg-muted/40">
              <span class="text-muted-foreground">{clock(entry.time)}</span>
              <span><Badge variant={levelTone[entry.level] ?? 'muted'}>{entry.level}</Badge></span>
              <button class="m-0 cursor-pointer truncate border-0 bg-transparent p-0 text-left font-mono text-xs text-primary" title={entry.matchId ? 'Filter to this match' : entry.source} onclick={() => entry.matchId && (matchFilter = entry.matchId)}>{entry.source}{entry.matchId ? `·${entry.matchId.slice(0, 6)}` : ''}</button>
              <span class="break-all" class:text-destructive={entry.level === 'ERROR'} class:text-warning={entry.level === 'WARN'}>{entry.message}{#if entry.attrs}{#each Object.entries(entry.attrs) as [k, v]}<span class="ml-2 text-muted-foreground">{k}=<span class="text-foreground/80">{v}</span></span>{/each}{/if}</span>
            </div>
          {:else}<p class="p-4 text-muted-foreground">No log entries match these filters yet.</p>{/each}
        </div>
        <div class="border-t border-border px-3 py-2 text-xs text-muted-foreground">{logs.length} entries shown · {paused ? 'paused' : 'live, refreshing every 2 s'} · in-memory, last 5,000 entries since the API started</div>
      </Card>

    {:else if tab === 'matches'}
      <Card>
        <CardHeader class="flex-row items-center justify-between"><CardTitle>Recent matches</CardTitle><Button size="sm" variant="secondary" onclick={refreshMatches}>Refresh</Button></CardHeader>
        <CardContent class="overflow-x-auto">
          <table class="w-full min-w-[760px] text-sm"><thead><tr class="text-left text-xs text-muted-foreground"><th class="py-1.5 font-normal">Match</th><th class="font-normal">Mode</th><th class="font-normal">Status</th><th class="font-normal">Robots</th><th class="font-normal">Created</th><th class="font-normal">Error</th><th></th></tr></thead>
            <tbody>{#each matches as m (m.matchId)}
              <tr class="border-t border-border align-top"><td class="py-2 font-mono text-xs">{m.matchId.slice(0, 8)}{#if m.engineVersion === 4}<Badge variant="muted" class="ml-1">v2</Badge>{/if}</td><td>{m.mode}</td><td><Badge variant={statusTone(m.status)}>{m.status}</Badge></td><td class="text-xs">{(m.robots ?? []).filter(r => !r.bot).map(r => r.displayName).join(', ') || 'bots only'}</td><td class="text-xs text-muted-foreground">{new Date(m.createdAt).toLocaleString()}</td><td class="max-w-xs text-xs text-destructive">{m.error ?? ''}</td><td class="whitespace-nowrap text-right"><Button size="sm" variant="ghost" onclick={() => workerLogs(m.matchId)}>Logs</Button><a class="ml-1 text-xs" href={`#/v2/${m.matchId}`}>Open</a></td></tr>
            {:else}<tr><td colspan="7" class="py-4 text-muted-foreground">No matches recorded.</td></tr>{/each}</tbody></table>
        </CardContent>
      </Card>

    {:else}
      <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] grid-cols-1">
        <Card>
          <CardHeader class="flex-row items-center justify-between"><CardTitle>Robot boxes</CardTitle><Button size="sm" variant="secondary" onclick={refreshBoxes}>Refresh</Button></CardHeader>
          <CardContent class="grid gap-2 grid-cols-1">
            {#each boxes as box (box.boxId)}
              <button class="m-0 flex w-full cursor-pointer items-center justify-between gap-3 rounded-lg border p-3 text-left {selectedBox === box.boxId ? 'border-primary bg-accent' : 'border-border bg-background hover:bg-muted/50'}" onclick={() => openBoxLogs(box.boxId)}>
                <span class="min-w-0"><span class="block truncate font-mono text-xs">{box.boxId}</span><span class="block text-xs text-muted-foreground">{box.status} · created {box.createdAt.split(' ').slice(0, 2).join(' ')}</span></span>
                <Badge variant={box.state === 'running' ? 'live' : 'muted'}>{box.state}</Badge>
              </button>
            {:else}<p class="m-0 text-sm text-muted-foreground">No robot boxes exist yet.</p>{/each}
          </CardContent>
        </Card>
        <Card class="overflow-hidden">
          <CardHeader class="flex-row items-center justify-between gap-3"><div><CardTitle>{selectedBox ? 'Box output' : 'Select a box'}</CardTitle><CardDescription>Supervisor and Lua agent stdout/stderr from Docker.</CardDescription></div>
            {#if selectedBox}<div class="flex items-center gap-2"><Select class="h-8 w-32 text-xs" bind:value={boxTail} ariaLabel="Lines"><option value={100}>100 lines</option><option value={300}>300 lines</option><option value={1000}>1000 lines</option></Select><Button size="sm" variant="secondary" onclick={() => openBoxLogs(selectedBox)} disabled={boxLoading}>{boxLoading ? 'Loading…' : 'Reload'}</Button></div>{/if}
          </CardHeader>
          <CardContent><pre class="m-0 h-[56vh] overflow-auto rounded-md bg-background p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">{selectedBox ? (boxLoading ? 'Loading…' : boxLogs) : 'Choose a box to read its recent output.'}</pre></CardContent>
        </Card>
      </div>
    {/if}
  {/if}
</section>
