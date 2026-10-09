<script lang="ts">
  import Button from './components/ui/button.svelte';
  import { localSignIn } from './auth';

  let { workos, onWorkos, onClose }: { workos: boolean; onWorkos: () => void; onClose: () => void } = $props();

  let mode = $state<'register' | 'login'>('register');
  let username = $state(''), password = $state(''), busy = $state(false), failure = $state('');

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    busy = true; failure = '';
    try {
      await localSignIn(mode, username, password);
      // A reload runs the normal session start-up (box, workspace, polling).
      window.location.reload();
    } catch (error) {
      failure = error instanceof Error ? error.message : String(error);
      busy = false;
    }
  }
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') onClose(); }} />

<div class="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
  <div class="w-full max-w-sm rounded-2xl border border-border bg-card p-6 shadow-2xl" role="dialog" aria-modal="true" aria-labelledby="sign-in-title">
    <h2 id="sign-in-title" class="m-0 text-xl font-semibold">{mode === 'register' ? 'Create a local account' : 'Sign in'}</h2>
    <p class="mt-1 mb-4 text-sm text-muted-foreground">Accounts are stored by this arena server. No email needed.</p>
    <div class="mb-4 grid grid-cols-2 gap-1 rounded-lg bg-muted p-1 text-sm" role="tablist">
      {#each [['register', 'Create account'], ['login', 'Sign in']] as [value, label]}
        <button type="button" role="tab" aria-selected={mode === value} class="rounded-md px-3 py-1.5 {mode === value ? 'bg-background text-foreground shadow' : 'text-muted-foreground'}" onclick={() => { mode = value as typeof mode; failure = ''; }}>{label}</button>
      {/each}
    </div>
    <form class="grid gap-3" onsubmit={submit}>
      <label class="grid gap-1 text-sm">Username
        <input class="rounded-md border border-border bg-background px-3 py-2 text-base" name="username" autocomplete="username" bind:value={username} minlength="3" maxlength="24" required />
      </label>
      <label class="grid gap-1 text-sm">Password
        <input class="rounded-md border border-border bg-background px-3 py-2 text-base" name="password" type="password" autocomplete={mode === 'register' ? 'new-password' : 'current-password'} bind:value={password} minlength="8" maxlength="128" required />
      </label>
      {#if failure}<p class="m-0 text-sm text-destructive" role="alert">{failure}</p>{/if}
      <Button type="submit" disabled={busy}>{busy ? 'Please wait…' : mode === 'register' ? 'Create account' : 'Sign in'}</Button>
    </form>
    {#if workos}
      <div class="my-4 flex items-center gap-3 text-xs text-muted-foreground"><span class="h-px flex-1 bg-border"></span>or<span class="h-px flex-1 bg-border"></span></div>
      <Button variant="outline" class="w-full" onclick={onWorkos}>Continue with WorkOS</Button>
    {/if}
  </div>
</div>
