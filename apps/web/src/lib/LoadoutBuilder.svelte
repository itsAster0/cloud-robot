<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from './api';
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import Badge from './components/ui/badge.svelte';
  import Progress from './components/ui/progress.svelte';

  interface Chassis { id: string; cost: number; hp: number; speed: number }
  interface Weapon { id: string; cost: number; damage: number; interval: number; range: number; heat: number; speed: number; pellets: number }
  interface Catalogue { budget: number; chassis: Chassis[]; weapons: Weapon[]; modules: string[]; utilities: [string, number][] }

  let { chassis = $bindable(), weapon = $bindable(), modules = $bindable(), utilities = $bindable(), busy, signedIn, onsave }: {
    chassis: string; weapon: string; modules: string[]; utilities: string[]; busy: boolean; signedIn: boolean; onsave: () => void;
  } = $props();

  let catalogue = $state<Catalogue | null>(null), error = $state('');
  onMount(() => { request<Catalogue>('/api/v4/catalogue').then(c => catalogue = c).catch(e => error = e instanceof Error ? e.message : String(e)); });

  // Effects mirror the Rust rules (simulation.rs module_stats and utilities).
  const moduleInfo: Record<string, string> = {
    reinforced_plating: '+20 max health',
    optics: 'Vision 600 → 900 units',
    capacitor: 'Energy 100 → 130',
    cooling_system: 'Weapons cool 25% faster',
    mobility_tuning: '+10% move speed',
    shield_reservoir: 'Max shield 50 → 75',
  };
  const utilityInfo: Record<string, string> = {
    cloak_emitter: 'Invisible for 5 s · 20 s cooldown',
    mine_dispenser: '3 proximity mines · arm after 3 s',
    smoke_projector: '2 smoke clouds that block sight for 8 s',
    repair_field: 'Heals nearby allies for 5 s · 20 s cooldown',
  };
  const presets: Record<string, { chassis: string; weapon: string; modules: string[]; utilities: string[]; note: string }> = {
    scout: { chassis: 'scout', weapon: 'machine_gun', modules: ['optics', 'mobility_tuning'], utilities: ['cloak_emitter'], note: 'Fast, sees far, disappears' },
    assault: { chassis: 'generalist', weapon: 'machine_gun', modules: ['reinforced_plating', 'cooling_system'], utilities: ['mine_dispenser'], note: 'Sustained close fire' },
    sniper: { chassis: 'generalist', weapon: 'railgun', modules: ['optics', 'cooling_system'], utilities: [], note: 'Long-range precision' },
    support: { chassis: 'generalist', weapon: 'plasma', modules: ['capacitor'], utilities: ['repair_field', 'smoke_projector'], note: 'Heals and screens a squad' },
  };
  const label = (id: string) => id.replace(/_/g, ' ');

  let budget = $derived(catalogue?.budget ?? 60);
  let chassisData = $derived(catalogue?.chassis.find(c => c.id === chassis));
  let weaponData = $derived(catalogue?.weapons.find(w => w.id === weapon));
  let utilityCost = (id: string) => catalogue?.utilities.find(([u]) => u === id)?.[1] ?? 10;
  let cost = $derived((chassisData?.cost ?? 0) + (weaponData?.cost ?? 0) + modules.length * 10 + utilities.reduce((n, id) => n + utilityCost(id), 0));
  let over = $derived(cost > budget);
  let hp = $derived((chassisData?.hp ?? 0) + (modules.includes('reinforced_plating') ? 20 : 0));
  let speed = $derived((chassisData?.speed ?? 0) * (modules.includes('mobility_tuning') ? 1.1 : 1));
  let dps = $derived(weaponData ? (weaponData.damage * weaponData.pellets) / weaponData.interval : 0);
  let maxHp = $derived(Math.max(...(catalogue?.chassis.map(c => c.hp) ?? [1])) + 20);
  let maxSpeed = $derived(Math.max(...(catalogue?.chassis.map(c => c.speed) ?? [1])) * 1.1);
  let maxDps = $derived(Math.max(...(catalogue?.weapons.map(w => (w.damage * w.pellets) / w.interval) ?? [1])));
  let maxRange = $derived(Math.max(...(catalogue?.weapons.map(w => w.range) ?? [1])));

  function toggle(list: string[], id: string): string[] {
    return list.includes(id) ? list.filter(x => x !== id) : list.length < 2 ? [...list, id] : list;
  }
  function applyPreset(name: string) {
    const p = presets[name];
    chassis = p.chassis; weapon = p.weapon; modules = [...p.modules]; utilities = [...p.utilities];
  }
  const option = (active: boolean, disabled = false) =>
    `flex w-full cursor-pointer flex-col gap-1 rounded-lg border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${active ? 'border-primary bg-accent' : 'border-border bg-background hover:border-input hover:bg-muted/60'}`;
</script>

<div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] grid-cols-1">
  <div class="grid content-start gap-5 grid-cols-1">
    {#if error}<p class="m-0 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-2.5 text-sm text-destructive">{error}</p>{/if}
    <Card>
      <CardHeader><CardTitle>Presets</CardTitle><CardDescription>Start from a role, then adjust any part.</CardDescription></CardHeader>
      <CardContent class="grid gap-2 sm:grid-cols-2 lg:grid-cols-4 grid-cols-1">
        {#each Object.entries(presets) as [name, p]}
          <button type="button" class={option(chassis === p.chassis && weapon === p.weapon && modules.join() === p.modules.join() && utilities.join() === p.utilities.join())} onclick={() => applyPreset(name)}>
            <span class="text-sm font-semibold capitalize">{name}</span><span class="text-xs text-muted-foreground">{p.note}</span>
          </button>
        {/each}
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>Chassis</CardTitle><CardDescription>Health against speed. Heavier bodies are harder to kill and slower to reposition.</CardDescription></CardHeader>
      <CardContent class="grid gap-2 sm:grid-cols-3 grid-cols-1">
        {#each catalogue?.chassis ?? [] as c (c.id)}
          <button type="button" class={option(chassis === c.id)} onclick={() => chassis = c.id} aria-pressed={chassis === c.id}>
            <span class="flex items-center justify-between"><span class="text-sm font-semibold capitalize">{c.id}</span><Badge variant="muted">{c.cost} pts</Badge></span>
            <span class="font-mono text-xs text-muted-foreground">{c.hp} HP · {c.speed} u/s</span>
          </button>
        {/each}
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>Starter weapon</CardTitle><CardDescription>Damage per second, effective range, and heat per shot. Overheating locks the weapon until it cools.</CardDescription></CardHeader>
      <CardContent class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 grid-cols-1">
        {#each catalogue?.weapons ?? [] as w (w.id)}
          <button type="button" class={option(weapon === w.id)} onclick={() => weapon = w.id} aria-pressed={weapon === w.id}>
            <span class="flex items-center justify-between"><span class="text-sm font-semibold capitalize">{label(w.id)}</span><Badge variant="muted">{w.cost} pts</Badge></span>
            <span class="font-mono text-xs text-muted-foreground">{Math.round((w.damage * w.pellets) / w.interval)} dps · {w.range} range · {w.heat} heat{w.pellets > 1 ? ` · ${w.pellets} pellets` : ''}{w.speed === 0 ? ' · hitscan' : ''}</span>
          </button>
        {/each}
      </CardContent>
    </Card>

    <div class="grid gap-5 lg:grid-cols-2 grid-cols-1">
      <Card>
        <CardHeader><CardTitle>Modules <span class="text-xs font-normal text-muted-foreground">up to 2 · 10 pts each</span></CardTitle></CardHeader>
        <CardContent class="grid gap-2 grid-cols-1">
          {#each catalogue?.modules ?? [] as id}
            <button type="button" class={option(modules.includes(id), !modules.includes(id) && modules.length >= 2)} disabled={!modules.includes(id) && modules.length >= 2} onclick={() => modules = toggle(modules, id)} aria-pressed={modules.includes(id)}>
              <span class="text-sm font-semibold capitalize">{label(id)}</span><span class="text-xs text-muted-foreground">{moduleInfo[id] ?? ''}</span>
            </button>
          {/each}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Utilities <span class="text-xs font-normal text-muted-foreground">up to 2</span></CardTitle></CardHeader>
        <CardContent class="grid gap-2 grid-cols-1">
          {#each catalogue?.utilities ?? [] as [id, points]}
            <button type="button" class={option(utilities.includes(id), !utilities.includes(id) && utilities.length >= 2)} disabled={!utilities.includes(id) && utilities.length >= 2} onclick={() => utilities = toggle(utilities, id)} aria-pressed={utilities.includes(id)}>
              <span class="flex items-center justify-between"><span class="text-sm font-semibold capitalize">{label(id)}</span><Badge variant="muted">{points} pts</Badge></span><span class="text-xs text-muted-foreground">{utilityInfo[id] ?? ''}</span>
            </button>
          {/each}
        </CardContent>
      </Card>
    </div>
  </div>

  <Card class="content-start xl:sticky xl:top-4">
    <CardHeader><CardTitle>Your robot</CardTitle><CardDescription class="capitalize">{chassis} · {label(weapon)}{modules.length ? ` · ${modules.map(label).join(', ')}` : ''}{utilities.length ? ` · ${utilities.map(label).join(', ')}` : ''}</CardDescription></CardHeader>
    <CardContent class="grid gap-4 grid-cols-1">
      <div class="grid gap-1.5 grid-cols-1">
        <div class="flex items-baseline justify-between"><span class="text-sm font-medium">Budget</span><span class="font-mono text-lg" class:text-destructive={over} class:text-primary={!over}>{cost}<span class="text-xs text-muted-foreground"> / {budget}</span></span></div>
        <Progress label="Points used" value={cost} max={budget} barClass={over ? 'bg-destructive' : 'bg-primary'}/>
        {#if over}<p class="m-0 text-xs text-destructive">Remove {cost - budget} points to save or register this build.</p>{/if}
      </div>
      <div class="grid gap-2.5 grid-cols-1">
        {#each [['Health', hp, maxHp, `${hp} HP`], ['Speed', speed, maxSpeed, `${Math.round(speed)} u/s`], ['Damage', dps, maxDps, `${Math.round(dps)} dps`], ['Range', weaponData?.range ?? 0, maxRange, `${weaponData?.range ?? 0} u`]] as [name, value, max, text]}
          <div class="grid gap-1 grid-cols-1"><div class="flex justify-between text-xs"><span class="text-muted-foreground">{name}</span><span class="font-mono">{text}</span></div><Progress label={String(name)} value={Number(value)} max={Number(max)}/></div>
        {/each}
      </div>
      <dl class="m-0 grid grid-cols-3 gap-2 text-center text-xs">
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-muted-foreground">Vision</dt><dd class="m-0 font-mono">{modules.includes('optics') ? 900 : 600}</dd></div>
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-muted-foreground">Energy</dt><dd class="m-0 font-mono">{modules.includes('capacitor') ? 130 : 100}</dd></div>
        <div class="rounded-lg bg-muted/60 p-2"><dt class="text-muted-foreground">Shield</dt><dd class="m-0 font-mono">{modules.includes('shield_reservoir') ? 75 : 50}</dd></div>
      </dl>
      <Button size="lg" onclick={onsave} disabled={busy || !signedIn || over}>{signedIn ? 'Save loadout' : 'Sign in to save'}</Button>
      <p class="m-0 text-xs text-muted-foreground">Registration uses this build. Your script can still pick up and equip other gear during a match.</p>
    </CardContent>
  </Card>
</div>
