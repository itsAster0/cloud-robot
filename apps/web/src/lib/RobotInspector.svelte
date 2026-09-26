<script lang="ts">
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Badge from './components/ui/badge.svelte';
  import Progress from './components/ui/progress.svelte';
  import Separator from './components/ui/separator.svelte';
  import { teamColor, weaponGlyph } from './matchStats';
  import type { RobotState } from './types';

  let { robot, tick = 0, tickRate = 20, youId = '', leaderId = '', onclose, onfollow }: {
    robot: RobotState | undefined; tick?: number; tickRate?: number; youId?: string; leaderId?: string;
    onclose?: () => void; onfollow?: (robotId: string) => void;
  } = $props();

  const label = (id: string) => id.replace(/^(weapon|utility):/, '').replace(/_/g, ' ');
  let speed = $derived(robot ? Math.hypot(robot.vx ?? 0, robot.vy ?? 0) : 0);
  let own = $derived(!!robot && robot.robotId === youId);
  let maxHp = $derived(robot?.maxHp ?? 100);
  let hpTone = $derived(!robot ? 'bg-primary' : robot.hp / maxHp > 0.5 ? 'bg-primary' : robot.hp / maxHp > 0.25 ? 'bg-warning' : 'bg-destructive');
</script>

<Card class="overflow-hidden">
  {#if !robot}
    <CardContent class="grid gap-2 p-5 text-sm text-muted-foreground">
      <strong class="text-foreground">Robot inspector</strong>
      <p class="m-0">Click a robot on the map or in the roster to see its health, weapon, combat stats, and position.</p>
    </CardContent>
  {:else}
    <div class="h-1" style:background={teamColor(robot.team)}></div>
    <CardHeader class="gap-2">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h3 class="m-0 truncate text-lg font-semibold">{robot.name}{#if robot.robotId === leaderId}<span class="ml-1.5 text-warning" title="Kill leader">♛</span>{/if}</h3>
          <p class="m-0 truncate font-mono text-[11px] text-muted-foreground">{robot.robotId}</p>
        </div>
        <Button size="icon" variant="ghost" ariaLabel="Close inspector" onclick={onclose}>×</Button>
      </div>
      <div class="flex flex-wrap gap-1.5">
        {#if robot.alive}<Badge variant="live">Alive</Badge>{:else}<Badge variant="danger">Eliminated{robot.placement ? ` · #${robot.placement}` : ''}</Badge>{/if}
        {#if own}<Badge variant="live">Your robot</Badge>{:else if robot.bot}<Badge variant="muted">Server bot</Badge>{:else if robot.bot === false}<Badge>Player script</Badge>{/if}
        <Badge variant="muted"><span class="inline-block size-2 rounded-full" style:background={teamColor(robot.team)}></span>{robot.team}</Badge>
        {#each robot.effects ?? [] as effect}<Badge variant="warn">{effect}</Badge>{/each}
      </div>
    </CardHeader>
    <CardContent class="grid gap-4">
      <div class="grid gap-2.5">
        <div class="grid gap-1"><div class="flex justify-between text-xs"><span class="text-muted-foreground">Health</span><span class="font-mono">{Math.round(robot.hp)} / {maxHp}</span></div><Progress label="Health" value={robot.hp} max={maxHp} barClass={hpTone}/></div>
        <div class="grid gap-1"><div class="flex justify-between text-xs"><span class="text-muted-foreground">Shield</span><span class="font-mono">{Math.round(robot.shield ?? 0)} / {robot.maxShield ?? 50}</span></div><Progress label="Shield" value={robot.shield ?? 0} max={robot.maxShield ?? 50} barClass="bg-team-blue"/></div>
        <div class="grid gap-1"><div class="flex justify-between text-xs"><span class="text-muted-foreground">Energy</span><span class="font-mono">{Math.round(robot.energy ?? 0)} / {robot.maxEnergy ?? 100}</span></div><Progress label="Energy" value={robot.energy ?? 0} max={robot.maxEnergy ?? 100} barClass="bg-warning"/></div>
      </div>
      <dl class="m-0 grid grid-cols-3 gap-2 text-center">
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-[10px] text-muted-foreground uppercase">Kills</dt><dd class="m-0 font-mono text-lg">{robot.kills ?? 0}</dd></div>
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-[10px] text-muted-foreground uppercase">Dealt</dt><dd class="m-0 font-mono text-lg">{Math.round(robot.damageDealt ?? 0)}</dd></div>
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-[10px] text-muted-foreground uppercase">Taken</dt><dd class="m-0 font-mono text-lg">{Math.round(robot.damageTaken ?? 0)}</dd></div>
      </dl>
      <dl class="m-0 grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
        <dt class="text-muted-foreground">Weapon</dt><dd class="m-0 text-right capitalize">{weaponGlyph(robot.weapon)} {label(robot.weapon ?? 'unknown')}</dd>
        <dt class="text-muted-foreground">Position</dt><dd class="m-0 text-right font-mono">{Math.round(robot.x)}, {Math.round(robot.y)}</dd>
        <dt class="text-muted-foreground">Speed</dt><dd class="m-0 text-right font-mono">{Math.round(speed)} u/s</dd>
        <dt class="text-muted-foreground">Heading · turret</dt><dd class="m-0 text-right font-mono">{Math.round(robot.heading)}° · {Math.round(robot.turretHeading ?? robot.heading)}°</dd>
        <dt class="text-muted-foreground">Vision</dt><dd class="m-0 text-right font-mono">{Math.round(robot.visionRange ?? 0)} u</dd>
      </dl>
      {#if robot.loadout || robot.weapons || robot.inventory}
        <Separator/>
        <div class="grid gap-3 text-sm">
          <p class="m-0 font-mono text-[10px] tracking-wider text-primary uppercase">Owner view</p>
          {#if robot.loadout}<p class="m-0 capitalize"><span class="text-muted-foreground">Build:</span> {label(robot.loadout.chassis)} · {[...robot.loadout.modules, ...robot.loadout.utilities].map(label).join(', ') || 'no modules'}</p>{/if}
          {#if robot.weapons?.length}
            <div class="grid gap-2">{#each robot.weapons as slot, index}
              {@const coolingTicks = Math.max(0, slot.readyAt - tick)}
              <div class="rounded-lg border p-2 {index === robot.activeWeapon ? 'border-primary/60' : 'border-border'}">
                <div class="flex justify-between text-xs"><span class="capitalize">Slot {index} · {label(slot.kind)}{index === robot.activeWeapon ? ' · active' : ''}</span><span class="font-mono {slot.overheated ? 'text-destructive' : 'text-muted-foreground'}">{slot.overheated ? 'overheated' : coolingTicks ? `ready in ${(coolingTicks / tickRate).toFixed(1)}s` : 'ready'}</span></div>
                <Progress class="mt-1.5 h-1.5" label="Heat" value={slot.heat} max={100} barClass={slot.overheated ? 'bg-destructive' : 'bg-warning'}/>
              </div>
            {/each}</div>
          {/if}
          {#if robot.inventory}<p class="m-0"><span class="text-muted-foreground">Inventory:</span> {robot.inventory.length ? robot.inventory.map((stack, slot) => `${slot}: ${label(stack.kind)} ×${stack.count}`).join(' · ') : 'empty'}</p>{/if}
          {#if robot.charges && Object.keys(robot.charges).length}<p class="m-0"><span class="text-muted-foreground">Charges:</span> {Object.entries(robot.charges).map(([id, count]) => `${label(id)} ${count}`).join(' · ')}</p>{/if}
          {#if robot.actionResults?.length}<div><p class="m-0 mb-1 text-muted-foreground">Last action results</p><pre class="m-0 max-h-32 overflow-auto rounded-md bg-background p-2 font-mono text-[11px]">{robot.actionResults.join('\n')}</pre></div>{/if}
        </div>
      {/if}
      {#if robot.lastAction || robot.logs?.length}
        <div class="grid gap-1 text-sm"><p class="m-0 text-muted-foreground">Last action</p><pre class="m-0 max-h-32 overflow-auto rounded-md bg-background p-2 font-mono text-[11px]">{[robot.lastAction, ...(robot.logs ?? [])].filter(Boolean).join('\n')}</pre></div>
      {/if}
      {#if robot.alive}<Button variant="secondary" class="w-full" onclick={() => onfollow?.(robot.robotId)}>Follow with camera</Button>{/if}
    </CardContent>
  {/if}
</Card>
