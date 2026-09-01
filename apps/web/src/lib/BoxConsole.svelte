<script lang="ts">
  import { onMount } from 'svelte';
  import type { User } from '@workos-inc/authkit-js';
  import { api } from './api';
  import type { RobotBox, ScriptTemplate } from './types';

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
  }

  let { user, box, sshKey = $bindable(''), pending, copiedSsh, authConfigured, mainSource, onProvision, onSaveKey, onRestart, onCopySsh, onLoadMain }: Props = $props();

  let scripts = $state<ScriptTemplate[]>([]);
  let scriptsLoaded = $state(false);
  let deploying = $state('');
  let scriptError = $state('');

  onMount(() => {
    void loadScripts();
  });

  async function loadScripts() {
    try {
      scripts = (await api.listScripts()).scripts;
    } catch {
      // Templates are a convenience; SSH editing keeps working without them.
    } finally {
      scriptsLoaded = true;
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
      case 'running': return 'AGENT RUNNING';
      case 'starting': return 'AGENT STARTING';
      case 'stopped': return 'AGENT STOPPED';
      case 'failed': return 'AGENT FAILED';
      case 'quota_exceeded': return 'QUOTA EXCEEDED';
      case 'idle': return 'NO AGENT CONFIGURED';
      default: return status.toUpperCase();
    }
  }

  function bytes(value = 0) { return `${(value / (1024 * 1024)).toFixed(1)} MB`; }
</script>

<section class="console-page">
  {#if !user}
    <div class="console-empty">
      <div class="eyebrow">BOX CONSOLE</div>
      <h1>Sign in to manage your box</h1>
      <p>{authConfigured ? 'Each account owns one persistent container with an SSH development workspace.' : 'WorkOS client ID is not configured; set VITE_WORKOS_CLIENT_ID and rebuild the web app.'}</p>
    </div>
  {:else if !box}
    <div class="console-empty">
      <div class="eyebrow">BOX CONSOLE</div>
      <h1>No box provisioned yet</h1>
      <p>Provisioning creates a persistent container bound to your account: 1 CPU, 512 MB RAM, 128 processes, 1 GB workspace.</p>
      <button class="deploy console-provision" onclick={onProvision} disabled={pending.provision}>{pending.provision ? 'PROVISIONING…' : 'PROVISION MY BOX'} <span>↗</span></button>
    </div>
  {:else}
    <header class="console-head">
      <div>
        <div class="eyebrow">BOX CONSOLE // {box.boxId.slice(0, 18)}</div>
        <h1><em>{box.sshUser}</em>@{box.sshHost}:{box.sshPort}</h1>
        <p class="console-sub">Persistent container · last checked {updated}</p>
      </div>
      <div class="console-badges">
        <span class="pill" data-tone={statusTone(box.status)}>{box.status.toUpperCase()}</span>
        <span class="pill" data-tone={statusTone(box.agentStatus)}>{agentLabel(box.agentStatus)}</span>
      </div>
    </header>

    <div class="console-grid">
      <article class="console-card connect-card">
        <div class="eyebrow">CONNECT OVER SSH</div>
        <code class="ssh-command">ssh -p {box.sshPort} {box.sshUser}@{box.sshHost}</code>
        <div class="row-actions">
          <button class="console-action" onclick={onCopySsh} disabled={pending.ssh}>{copiedSsh ? 'COPIED ✓' : 'COPY COMMAND'}</button>
          <button class="console-action" onclick={onRestart} disabled={pending.restart}>{pending.restart ? 'RESTARTING…' : 'RESTART BOX'}</button>
        </div>
        <p class="hint">Key-only access. The workspace survives restarts; robot credentials are never exposed here.</p>
      </article>

      <article class="console-card">
        <div class="eyebrow">RESOURCES</div>
        <div class="meter" class:critical={storageCritical}>
          <div class="meter-fill" style="width: {storagePercent}%"></div>
        </div>
        <div class="meter-caption"><span>WORKSPACE {bytes(box.usageBytes)} / {bytes(box.limits.storageBytes)}</span><span>{storagePercent}%</span></div>
        <div class="limit-grid">
          <div><span>CPU</span><strong>{box.limits.cpus} core</strong></div>
          <div><span>MEMORY</span><strong>{box.limits.memoryMb} MB</strong></div>
          <div><span>PROCESSES</span><strong>{box.limits.pids} PIDs</strong></div>
        </div>
        {#if box.agentStatus === 'quota_exceeded'}
          <p class="warn">Workspace quota exceeded. The agent is stopped; delete files over SSH, then restart the box.</p>
        {/if}
      </article>

      <article class="console-card">
        <div class="eyebrow">BOX AGENT</div>
        <div class="agent-state"><strong>{agentLabel(box.agentStatus)}</strong></div>
        <div class="limit-grid">
          <div><span>ACTIVE ROBOT</span><strong>{box.activeRobotId ? box.activeRobotId.slice(0, 8) : '—'}</strong></div>
          <div><span>ACTIVE MATCH</span><strong>{box.activeMatchId ? box.activeMatchId.slice(0, 8) : '—'}</strong></div>
        </div>
        <p class="hint">Registration configures the agent automatically; it reconnects on network loss without any browser involvement.</p>
      </article>

      <article class="console-card key-card">
        <div class="eyebrow">SSH ACCESS KEY</div>
        {#if box.keyFingerprint}
          <p class="hint">Installed fingerprint</p>
          <code class="fingerprint">{box.keyFingerprint}</code>
        {:else}
          <p class="warn">No key installed yet — SSH will reject until you add one.</p>
        {/if}
        <label class="console-label" for="console-ssh-key">Replace or install public key</label>
        <textarea id="console-ssh-key" class="console-textarea" bind:value={sshKey} rows="3" placeholder="ssh-ed25519 AAAA… you@example.com"></textarea>
        <div class="row-actions">
          <button class="console-action primary" onclick={onSaveKey} disabled={pending.key || !sshKey.trim()}>{pending.key ? 'SAVING…' : (box.keyFingerprint ? 'REPLACE KEY' : 'ADD KEY')}</button>
        </div>
        <p class="hint">One line, OpenSSH public format (Ed25519, RSA, ECDSA), at most 8 KiB.</p>
      </article>

      <article class="console-card workspace-card">
        <div class="workspace-head"><div class="eyebrow">/WORKSPACE/MAIN.LUA</div><button class="console-action" onclick={onLoadMain} disabled={pending.main}>{mainSource === null ? (pending.main ? 'LOADING…' : 'VIEW FILE') : 'REFRESH'}</button></div>
        {#if mainSource !== null}
          <pre class="console-pre"><code>{mainSource}</code></pre>
        {:else}
          <p class="hint">A demo robot ships with the box. SSH in to edit it; the server snapshots this file to S3 when you register a robot.</p>
        {/if}
      </article>

      <article class="console-card scripts-card">
        <div class="workspace-head"><div class="eyebrow">ROBOT SCRIPTS // ONE-CLICK DEPLOY</div><span class="hint-inline">OVERWRITES MAIN.LUA</span></div>
        {#if scriptError}<p class="warn">{scriptError}</p>{/if}
        {#each scripts as script (script.name)}
          <div class="script-row">
            <div><strong>{script.name}</strong><p>{script.description}</p></div>
            <button class="console-action" onclick={() => deployScript(script)} disabled={!!deploying || box.status !== 'running'}>{deploying === script.name ? 'DEPLOYING…' : 'DEPLOY'}</button>
          </div>
        {:else}
          <p class="hint">{scriptsLoaded ? 'No templates available.' : 'Loading templates…'}</p>
        {/each}
        <p class="hint">Deploy writes the template into your box workspace, the preview updates, and the next registration snapshots it. Editing over SSH still works.</p>
      </article>
    </div>

    {#if box.error}
      <div class="console-alert" role="alert"><span>BOX FAULT</span>{box.error}</div>
    {/if}
  {/if}
</section>
