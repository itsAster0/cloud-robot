<script lang="ts">
  import { onMount } from 'svelte';
  import type { User } from '@workos-inc/authkit-js';
  import { api } from './api';
  import type { RobotBox, ScriptTemplate, ScriptVersion } from './types';
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Badge from './components/ui/badge.svelte';
  import Progress from './components/ui/progress.svelte';
  import BoxMonitor from './BoxMonitor.svelte';
  import BoxLogs from './BoxLogs.svelte';
  import BoxFiles from './BoxFiles.svelte';

  interface Props {
    user: User | null;
    box: RobotBox | null;
    sshKey: string;
    pending: Record<string, boolean>;
    copiedSsh: boolean;
    authConfigured: boolean;
    mainSource: string | null;
    onProvision: () => void;
    onSaveKey: () => void;
    onRestart: () => void;
    onCopySsh: () => void;
    onLoadMain: () => void;
    onWithdrawMatch: () => void;
    checking?: boolean;
  }

  let { user, box, checking = false, sshKey = $bindable(''), pending, copiedSsh, authConfigured, mainSource, onProvision, onSaveKey, onRestart, onCopySsh, onLoadMain, onWithdrawMatch }: Props = $props();

  let scripts = $state<ScriptTemplate[]>([]);
  let scriptsLoaded = $state(false);
  let deploying = $state('');
  let scriptError = $state('');
  let versions = $state<ScriptVersion[]>([]);
  let restoring = $state('');
  let showKeyForm = $state(false);

  onMount(() => { void loadScripts(); });
  // The box record usually arrives after mount; load its history and output
  // once per box.
  let loadedFor = '';
  $effect(() => {
    const id = user && box ? box.boxId : '';
    if (!id || id === loadedFor) return;
    loadedFor = id;
    void loadVersions();
  });

  async function loadScripts() {
    try {
      // Balanced (template v4) is the recommended starting point; list it first.
      scripts = (await api.listScripts()).scripts.sort((a, b) => Number(b.name === 'v4') - Number(a.name === 'v4'));
    } catch {
      // Templates are a convenience; SSH editing keeps working without them.
    } finally {
      scriptsLoaded = true;
    }
  }

  async function loadVersions() {
    try {
      versions = (await api.listScriptVersions()).versions;
    } catch (failure) {
      scriptError = failure instanceof Error ? failure.message : String(failure);
    }
  }

  async function restoreVersion(version: ScriptVersion) {
    if (restoring) return;
    const label = `${new Date(version.createdAt).toLocaleString()} (${version.versionId.slice(0, 8)})`;
    if (!confirm(`Restore /workspace/main.lua from ${label}? The current file is overwritten.`)) return;
    restoring = version.versionId;
    scriptError = '';
    try {
      await api.restoreScriptVersion(version.versionId);
      onLoadMain();
      await loadVersions();
    } catch (failure) {
      scriptError = failure instanceof Error ? failure.message : String(failure);
    } finally {
      restoring = '';
    }
  }

  async function deployScript(script: ScriptTemplate) {
    if (deploying) return;
    deploying = script.name;
    scriptError = '';
    try {
      await api.writeBoxMain(script.name);
      onLoadMain();
    } catch (failure) {
      scriptError = failure instanceof Error ? failure.message : String(failure);
    } finally {
      deploying = '';
    }
  }

  let storagePercent = $derived.by(() => {
    if (!box || box.limits.storageBytes <= 0) return 0;
    return Math.min(100, Math.round((box.usageBytes / box.limits.storageBytes) * 100));
  });
  let storageCritical = $derived(storagePercent >= 90);
  let updated = $derived(box ? new Date(box.updatedAt).toLocaleTimeString() : '--');

  function statusTone(status: string) {
    if (status === 'running' || status === 'connected' || status === 'running_agent') return 'ok';
    if (status === 'quota_exceeded' || status === 'failed') return 'bad';
    return 'idle';
  }

  function agentLabel(status: string) {
    switch (status) {
      case 'running': return 'Agent running';
      case 'starting': return 'Agent starting';
      case 'stopped': return 'Agent stopped';
      case 'failed': return 'Agent failed';
      case 'quota_exceeded': return 'Quota exceeded';
      case 'idle': return 'Agent idle';
      default: return status;
    }
  }
  function agentHelp(status: string) {
    switch (status) {
      case 'running': return 'Your robot script is connected and playing.';
      case 'starting': return 'The box is launching your script.';
      case 'failed': return 'The script process exited with an error. Check the box output below.';
      case 'quota_exceeded': return 'The workspace is over its storage quota. Delete files over SSH, then restart the box.';
      case 'stopped': return 'The script exited. Registering for a match starts it again.';
      default: return 'Nothing is running. Registering a robot in a match starts your saved main.lua here.';
    }
  }
  const tone = (status: string) => statusTone(status) === 'ok' ? 'live' : statusTone(status) === 'bad' ? 'danger' : 'muted';
  let lineCount = $derived(mainSource ? mainSource.split('\n').length : 0);

  function bytes(value = 0) { return `${(value / (1024 * 1024)).toFixed(1)} MB`; }
</script>

<section class="mx-auto grid max-w-[1200px] gap-5 px-4 py-8 sm:px-8 grid-cols-1">
  {#if !user}
    <Card class="mx-auto mt-10 max-w-lg"><CardHeader><CardTitle>Sign in to manage your box</CardTitle><CardDescription>{authConfigured ? 'Each account owns one persistent container with an SSH development workspace.' : 'WorkOS client ID is not configured; set VITE_WORKOS_CLIENT_ID and rebuild the web app.'}</CardDescription></CardHeader></Card>
  {:else if !box && checking}
    <div class="grid min-h-[40vh] place-items-center text-sm text-muted-foreground" role="status"><span class="flex items-center gap-2"><span class="size-2 animate-pulse rounded-full bg-primary"></span>Loading your box…</span></div>
  {:else if !box}
    <Card class="mx-auto mt-10 max-w-lg">
      <CardHeader><CardTitle>Create your robot box</CardTitle><CardDescription>A persistent container bound to your account, with an SSH workspace where your robot's code lives.</CardDescription></CardHeader>
      <CardContent class="grid gap-4 grid-cols-1">
        <div class="grid grid-cols-4 gap-2 text-center text-xs">{#each [['1', 'CPU'], ['512 MB', 'memory'], ['128', 'processes'], ['1 GB', 'workspace']] as [value, label]}<div class="rounded-lg bg-muted/60 p-2"><strong class="block font-mono text-sm">{value}</strong><span class="text-muted-foreground">{label}</span></div>{/each}</div>
        <Button size="lg" onclick={onProvision} disabled={pending.provision}>{pending.provision ? 'Creating your box…' : 'Create my box'}</Button>
      </CardContent>
    </Card>
  {:else}
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <p class="m-0 font-mono text-[11px] tracking-widest text-muted-foreground">YOUR ROBOT BOX · {box.boxId.slice(10, 18)}</p>
        <h1 class="m-0 text-2xl font-semibold tracking-tight break-all sm:truncate sm:text-3xl sm:break-normal"><span class="text-primary">{box.sshUser}</span>@{box.sshHost}:{box.sshPort}</h1>
        <p class="m-0 mt-1 text-sm text-muted-foreground">Persistent container · checked {updated}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Badge variant={tone(box.status)}>Box {box.status}</Badge>
        <Badge variant={tone(box.agentStatus)}>{agentLabel(box.agentStatus)}</Badge>
        {#if box.activeMatchId}<a href={`#/v2/${box.activeMatchId}`}><Badge variant="live">In match →</Badge></a>{/if}
        <Button size="sm" variant="secondary" onclick={onRestart} disabled={pending.restart}>{pending.restart ? 'Restarting…' : 'Restart box'}</Button>
      </div>
    </header>

    {#if box.error}<div class="rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-2.5 text-sm text-destructive" role="alert"><strong>Box fault:</strong> {box.error}</div>{/if}

    <div class="grid gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)] grid-cols-1">
      <Card>
        <CardHeader><CardTitle>Connect over SSH</CardTitle><CardDescription>Edit <code>/workspace/main.lua</code> with any editor, or use the Workspace code editor in the browser.</CardDescription></CardHeader>
        <CardContent class="grid gap-4 grid-cols-1">
          <div class="flex items-center gap-2 rounded-lg border border-border bg-background p-2 pl-3">
            <code class="min-w-0 flex-1 truncate font-mono text-sm">ssh -p {box.sshPort} {box.sshUser}@{box.sshHost}</code>
            <Button size="sm" variant={copiedSsh ? 'secondary' : 'default'} onclick={onCopySsh} disabled={pending.ssh}>{copiedSsh ? 'Copied ✓' : 'Copy'}</Button>
          </div>
          <ol class="m-0 grid gap-2 pl-0 text-sm grid-cols-1">
            <li class="flex items-start gap-3"><Badge variant={box.keyFingerprint ? 'live' : 'warn'}>{box.keyFingerprint ? '✓' : '1'}</Badge><span>{box.keyFingerprint ? 'SSH key installed.' : 'Add your SSH public key below.'}</span></li>
            <li class="flex items-start gap-3"><Badge variant="muted">2</Badge><span>Connect with the command above and edit <code>main.lua</code>, or deploy a strategy below.</span></li>
            <li class="flex items-start gap-3"><Badge variant="muted">3</Badge><span>Register in a lobby: the server snapshots your file and starts it here.</span></li>
          </ol>
          <div class="grid gap-2 border-t border-border pt-4 grid-cols-1">
            <div class="flex items-center justify-between gap-3">
              <div class="min-w-0"><p class="m-0 text-sm font-medium">SSH key</p><p class="m-0 truncate font-mono text-xs text-muted-foreground">{box.keyFingerprint ?? 'No key installed; SSH rejects connections until you add one.'}</p></div>
              {#if box.keyFingerprint}<Button size="sm" variant="ghost" onclick={() => showKeyForm = !showKeyForm}>{showKeyForm ? 'Cancel' : 'Replace key'}</Button>{/if}
            </div>
            {#if !box.keyFingerprint || showKeyForm}
              <textarea class="m-0 min-h-20 w-full rounded-md border border-input bg-background p-3 font-mono text-xs text-foreground outline-none focus:border-ring" bind:value={sshKey} rows="3" placeholder="ssh-ed25519 AAAA… you@example.com" aria-label="SSH public key"></textarea>
              <div class="flex items-center justify-between gap-3"><span class="text-xs text-muted-foreground">One line, OpenSSH format (Ed25519, RSA, ECDSA), at most 8 KiB.</span><Button size="sm" onclick={() => { onSaveKey(); showKeyForm = false; }} disabled={pending.key || !sshKey.trim()}>{pending.key ? 'Saving…' : box.keyFingerprint ? 'Replace key' : 'Add key'}</Button></div>
            {/if}
          </div>
        </CardContent>
      </Card>

      <div class="grid content-start gap-5 grid-cols-1">
        <Card>
          <CardHeader><CardTitle>{agentLabel(box.agentStatus)}</CardTitle><CardDescription>{agentHelp(box.agentStatus)}</CardDescription></CardHeader>
          <CardContent class="grid gap-3 grid-cols-1">
            <dl class="m-0 grid grid-cols-2 gap-2 text-sm">
              <div class="rounded-lg bg-muted/60 p-2.5"><dt class="text-xs text-muted-foreground">Active robot</dt><dd class="m-0 font-mono">{box.activeRobotId ? box.activeRobotId.slice(0, 8) : '—'}</dd></div>
              <div class="rounded-lg bg-muted/60 p-2.5"><dt class="text-xs text-muted-foreground">Active match</dt><dd class="m-0 font-mono">{#if box.activeMatchId}<a href={`#/v2/${box.activeMatchId}`}>{box.activeMatchId.slice(0, 8)}</a>{:else}—{/if}</dd></div>
            </dl>
            {#if box.activeMatchId}
              <p class="m-0 text-xs text-warning">Queueing and new registrations are blocked until this match ends.</p>
              <Button variant="destructive" onclick={onWithdrawMatch} disabled={pending['box-withdraw']}>{pending['box-withdraw'] ? 'Withdrawing…' : 'Withdraw from match'}</Button>
            {/if}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Resources</CardTitle></CardHeader>
          <CardContent class="grid gap-3 grid-cols-1">
            <div class="grid gap-1.5 grid-cols-1"><div class="flex justify-between text-xs"><span class="text-muted-foreground">Workspace</span><span class="font-mono">{bytes(box.usageBytes)} / {bytes(box.limits.storageBytes)} · {storagePercent}%</span></div><Progress label="Workspace usage" value={storagePercent} barClass={storageCritical ? 'bg-destructive' : 'bg-primary'}/></div>
            <div class="grid grid-cols-3 gap-2 text-center text-xs">
              <div class="rounded-lg bg-muted/60 p-2"><strong class="block font-mono text-sm">{box.limits.cpus}</strong><span class="text-muted-foreground">CPU</span></div>
              <div class="rounded-lg bg-muted/60 p-2"><strong class="block font-mono text-sm">{box.limits.memoryMb} MB</strong><span class="text-muted-foreground">memory</span></div>
              <div class="rounded-lg bg-muted/60 p-2"><strong class="block font-mono text-sm">{box.limits.pids}</strong><span class="text-muted-foreground">processes</span></div>
            </div>
            {#if box.agentStatus === 'quota_exceeded'}<p class="m-0 text-xs text-destructive">Workspace quota exceeded. The agent is stopped; delete files over SSH, then restart the box.</p>{/if}
          </CardContent>
        </Card>
      </div>
    </div>

    <BoxMonitor/>
    <BoxLogs/>
    <BoxFiles/>

    <Card class="overflow-hidden">
      <CardHeader class="flex-row items-center justify-between gap-3"><div><CardTitle><code>/workspace/main.lua</code></CardTitle><CardDescription>{mainSource === null ? 'The file your next registration snapshots.' : `${lineCount} lines · ${mainSource.length.toLocaleString()} bytes`}</CardDescription></div>
        <div class="flex gap-2"><a class="inline-flex h-8 items-center rounded-md px-3 text-xs" href="#/workspace">Open in editor</a><Button size="sm" variant="secondary" onclick={onLoadMain} disabled={pending.main}>{mainSource === null ? (pending.main ? 'Loading…' : 'View file') : 'Refresh'}</Button></div></CardHeader>
      {#if mainSource !== null}<CardContent><pre class="m-0 max-h-96 overflow-auto rounded-md bg-background p-3 font-mono text-xs leading-relaxed"><code>{mainSource}</code></pre></CardContent>{/if}
    </Card>

    <Card>
      <CardHeader><CardTitle>Strategies</CardTitle><CardDescription>Deploying overwrites <code>main.lua</code>. Your previous file stays in the version history below.</CardDescription></CardHeader>
      <CardContent class="grid gap-3 grid-cols-1">
        {#if scriptError}<p class="m-0 text-sm text-destructive">{scriptError}</p>{/if}
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 grid-cols-1">
          {#each scripts as script (script.name)}
            <div class="flex flex-col justify-between gap-3 rounded-lg border border-border bg-background p-3">
              <div>
                <p class="m-0 flex items-center gap-2 text-sm font-semibold capitalize">{script.name === 'v4' ? 'balanced' : script.name.replace('-', ' ')}{#if script.files}<Badge variant="muted">{Object.keys(script.files).length + 1} files</Badge>{/if}</p>
                <p class="m-0 mt-1 text-xs text-muted-foreground">{script.description}</p>
                {#if script.files}<p class="m-0 mt-2 font-mono text-[11px] text-muted-foreground">main.lua · {Object.keys(script.files).sort().join(' · ')}</p>{/if}
              </div>
              <Button size="sm" variant="secondary" onclick={() => deployScript(script)} disabled={!!deploying || box.status !== 'running'}>{deploying === script.name ? 'Deploying…' : 'Deploy'}</Button>
            </div>
          {:else}<p class="m-0 text-sm text-muted-foreground">{scriptsLoaded ? 'No templates available.' : 'Loading templates…'}</p>{/each}
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader class="flex-row items-center justify-between"><div><CardTitle>Version history</CardTitle><CardDescription>Every deploy and registration saves a version to S3.</CardDescription></div><Button size="sm" variant="ghost" onclick={loadVersions}>Refresh</Button></CardHeader>
      <CardContent class="grid gap-2 grid-cols-1">
        {#each versions.slice(0, 12) as version (version.versionId)}
          <div class="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2">
            <span class="text-sm">{new Date(version.createdAt).toLocaleString()} <span class="ml-2 font-mono text-xs text-muted-foreground">{version.versionId.slice(0, 12)}</span></span>
            <Button size="sm" variant="ghost" onclick={() => restoreVersion(version)} disabled={!!restoring || box.status !== 'running'}>{restoring === version.versionId ? 'Restoring…' : 'Restore'}</Button>
          </div>
        {:else}<p class="m-0 text-sm text-muted-foreground">No saved versions yet. Deploy or register a robot to create one.</p>{/each}
      </CardContent>
    </Card>
  {/if}
</section>
