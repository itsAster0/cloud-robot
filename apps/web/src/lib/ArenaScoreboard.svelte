<script lang="ts">
  import { teamColor } from './matchStats';
  import type { RobotState } from './types';

  // Arena standings: kills first, then fewer deaths, then damage dealt.
  let { robots, youId = '', endTick = 0, tick = 0, tickRate = 20, onselect }: { robots: RobotState[]; youId?: string; endTick?: number; tick?: number; tickRate?: number; onselect?: (id: string) => void } = $props();
  const ranked = $derived([...robots].sort((a, b) => (b.kills ?? 0) - (a.kills ?? 0) || (a.deaths ?? 0) - (b.deaths ?? 0) || (b.damageDealt ?? 0) - (a.damageDealt ?? 0)));
  const you = $derived(robots.find(r => r.robotId === youId));
  const remaining = $derived(Math.max(0, Math.ceil((endTick - tick) / tickRate)));
  const clock = $derived(`${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, '0')}`);
  const players = $derived(robots.filter(r => !r.bot).length);
</script>

<div class="grid gap-3 rounded-2xl border border-border bg-card p-4" aria-label="Arena scoreboard">
  <div class="flex flex-wrap items-baseline justify-between gap-2">
    <h3 class="m-0 text-base font-semibold">Scoreboard</h3>
    <span class="font-mono text-xs text-muted-foreground">{players} {players === 1 ? 'player' : 'players'} · {robots.length - players} bots{#if endTick} · new map in {clock}{/if}</span>
  </div>
  {#if you && !you.alive && you.respawnIn != null}
    <div class="respawn flex items-center gap-3 rounded-xl border border-primary/40 bg-accent p-3 text-sm" role="status">
      <span class="grid size-9 place-items-center rounded-full bg-primary font-mono font-semibold text-primary-foreground">{Math.ceil(you.respawnIn / tickRate)}</span>
      <span>Your robot was destroyed. It respawns at a safe spot in {Math.ceil(you.respawnIn / tickRate)}s and keeps its score.</span>
    </div>
  {/if}
  <ol class="m-0 grid max-h-80 gap-1 overflow-auto p-0">
    <li class="grid grid-cols-[2rem_minmax(0,1fr)_3rem_3rem_4rem] items-center gap-2 px-2 text-[11px] tracking-wide text-muted-foreground uppercase"><span>#</span><span>Robot</span><span class="text-right">K</span><span class="text-right">D</span><span class="text-right">Dmg</span></li>
    {#each ranked as robot, i (robot.robotId)}
      <li class="list-none">
        <button type="button" onclick={() => onselect?.(robot.robotId)}
          class="m-0 grid w-full cursor-pointer grid-cols-[2rem_minmax(0,1fr)_3rem_3rem_4rem] items-center gap-2 rounded-lg border-0 px-2 py-1.5 text-left text-sm text-foreground {robot.robotId === youId ? 'bg-accent ring-1 ring-primary' : 'bg-transparent hover:bg-muted'}">
          <span class="font-mono text-xs text-muted-foreground">{i + 1}</span>
          <span class="flex min-w-0 items-center gap-2">
            <i class="size-2.5 shrink-0 rounded-full" style:background={teamColor(robot.team)}></i>
            <span class="truncate" class:opacity-50={!robot.alive}>{robot.name}</span>
            {#if robot.robotId === youId}<span class="rounded bg-primary px-1 text-[10px] font-semibold text-primary-foreground">YOU</span>{:else if robot.bot}<span class="text-[10px] text-muted-foreground">bot</span>{/if}
            {#if !robot.alive && robot.respawnIn != null}<span class="font-mono text-[10px] text-muted-foreground">↻ {Math.ceil(robot.respawnIn / tickRate)}s</span>{/if}
          </span>
          <span class="text-right font-mono font-semibold">{robot.kills ?? 0}</span>
          <span class="text-right font-mono text-muted-foreground">{robot.deaths ?? 0}</span>
          <span class="text-right font-mono text-xs text-muted-foreground">{Math.round(robot.damageDealt ?? 0)}</span>
        </button>
      </li>
    {/each}
  </ol>
</div>

<style>
  .respawn { animation: pulse 1s ease-in-out infinite alternate; }
  @keyframes pulse { to { border-color: var(--color-primary); } }
  @media (prefers-reduced-motion: reduce) { .respawn { animation: none; } }
</style>
