<script lang="ts">
  import MapPreview, { type MapPreviewData } from './MapPreview.svelte';
  import Button from './components/ui/button.svelte';
  import Card from './components/ui/card.svelte';
  import CardHeader from './components/ui/card-header.svelte';
  import CardTitle from './components/ui/card-title.svelte';
  import CardDescription from './components/ui/card-description.svelte';
  import CardContent from './components/ui/card-content.svelte';
  import ChoiceGroup from './components/ui/choice-group.svelte';
  import Slider from './components/ui/slider.svelte';
  import Input from './components/ui/input.svelte';
  import Badge from './components/ui/badge.svelte';
  import Separator from './components/ui/separator.svelte';

  let {
    mode = $bindable(), capacity = $bindable(), size = $bindable(), duration = $bindable(),
    siteCount = $bindable(), coverPerSite = $bindable(), lootPerSite = $bindable(), seed = $bindable(), liveEdit = $bindable(),
    signedIn, busy, preview, previewLoading, testing = false,
    onmodechange, oncreate, onrandomize, onSignIn,
  }: {
    mode: string; capacity: number; size: number; duration: number;
    siteCount: number; coverPerSite: number; lootPerSite: number; seed: number; liveEdit: boolean;
    signedIn: boolean; busy: boolean; preview: MapPreviewData | null; previewLoading: boolean; testing?: boolean;
    onmodechange: () => void; oncreate: () => void; onrandomize: () => void; onSignIn?: () => void;
  } = $props();

  const modes = [
    { value: 'sandbox', label: 'Sandbox', icon: '⧉', meta: 'PRACTICE', description: 'Pause, step, and inspect every tick. Best for testing a new script.' },
    { value: 'quick-duel', label: 'Quick duel', icon: '⇄', meta: '1 V 1', description: 'One opponent on a compact map. A server bot fills the empty side.' },
    { value: 'br-solo', label: 'Solo battle royale', icon: '◎', meta: 'FFA', description: 'Every robot for itself on a large world. Last one standing wins.' },
    { value: 'br-squad', label: 'Squad battle royale', icon: '◈', meta: 'TEAMS OF 4', description: 'Squads coordinate with messages. The last squad alive wins.' },
  ];
  const sizes = [
    { value: 20, label: 'Skirmish', detail: '24k × 15k', robots: 32 },
    { value: 30, label: 'Standard', detail: '36k × 22.5k', robots: 64 },
    { value: 35, label: 'Large', detail: '42k × 26k', robots: 128 },
    { value: 40, label: 'Max', detail: '48k × 30k', robots: 256 },
  ];
  const density = [{ value: 3, label: 'Open' }, { value: 8, label: 'Normal' }, { value: 12, label: 'Dense' }];
  const loot = [{ value: 8, label: 'Scarce' }, { value: 16, label: 'Normal' }, { value: 28, label: 'Rich' }];

  let large = $derived(mode === 'br-solo' || mode === 'br-squad');
  let squadStep = $derived(mode === 'br-squad' ? 4 : 1);
  let robotPicks = $derived(mode === 'sandbox' ? [1, 2, 4, 8] : mode === 'br-squad' ? [8, 16, 32, 64, 128] : [8, 16, 32, 64, 128, 256]);
  let lengths = $derived(large ? [{ value: 300, label: '5 min' }, { value: 600, label: '10 min' }, { value: 1080, label: '18 min' }, { value: 1800, label: '30 min' }]
    : [{ value: 120, label: '2 min' }, { value: 180, label: '3 min' }, { value: 300, label: '5 min' }, { value: 600, label: '10 min' }]);
  let sizeInfo = $derived(sizes.find(entry => entry.value === size));
  let crowded = $derived(large && sizeInfo !== undefined && capacity > sizeInfo.robots);
  let minutes = $derived(duration / 60);
  let world = $derived(large ? `${(1200 * size).toLocaleString()} × ${(750 * size).toLocaleString()}` : '2,400 × 1,500');
</script>

<div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(300px,380px)] grid-cols-1">
  <div class="grid content-start gap-5 grid-cols-1">
    <Card>
      <CardHeader><CardTitle>1 · Choose a mode</CardTitle><CardDescription>{testing ? 'Sandbox is recommended for debugging: you can pause and step the simulation.' : 'All Arena V2 modes are unranked practice while the platform is in review.'}</CardDescription></CardHeader>
      <CardContent><ChoiceGroup ariaLabel="Game mode" class="sm:grid-cols-2" bind:value={mode} choices={modes} onchange={onmodechange}/></CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>2 · Robots and world</CardTitle><CardDescription>You register your own robot after creating the lobby. Empty slots become server bots.</CardDescription></CardHeader>
      <CardContent class="grid gap-5 grid-cols-1">
        {#if mode === 'quick-duel'}
          <p class="m-0 text-sm text-muted-foreground">Duels always have <strong class="text-foreground">2 robots</strong> on the practice map.</p>
        {:else}
          <div class="grid gap-2 grid-cols-1">
            <div class="flex items-baseline justify-between"><span class="text-sm font-medium">Robot slots</span><span class="font-mono text-lg text-primary">{capacity}</span></div>
            <Slider ariaLabel="Robot slots" bind:value={capacity} min={mode === 'br-squad' ? 4 : 1} max={mode === 'sandbox' ? 16 : 256} step={squadStep}/>
            <div class="flex flex-wrap gap-1.5">{#each robotPicks as pick}<Button size="sm" variant={capacity === pick ? 'default' : 'outline'} onclick={() => capacity = pick}>{pick}</Button>{/each}</div>
            {#if mode === 'br-squad'}<p class="m-0 text-xs text-muted-foreground">{capacity / 4} squads of 4.</p>{/if}
          </div>
        {/if}
        {#if large}
          <div class="grid gap-2 grid-cols-1">
            <span class="text-sm font-medium">World size</span>
            <ChoiceGroup ariaLabel="World size" layout="segmented" bind:value={size} choices={sizes.map(entry => ({ value: entry.value, label: entry.label }))}/>
            <p class="m-0 text-xs text-muted-foreground">{sizeInfo?.detail ?? world} units · comfortable for up to {sizeInfo?.robots ?? '—'} robots.</p>
            {#if crowded}<p class="m-0 text-xs text-warning">This many robots will feel crowded. Pick a larger world or fewer slots.</p>{/if}
          </div>
        {/if}
        <div class="grid gap-2 grid-cols-1">
          <span class="text-sm font-medium">Match length</span>
          <ChoiceGroup ariaLabel="Match length" layout="segmented" bind:value={duration} choices={lengths}/>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>3 · Map</CardTitle><CardDescription>Maps are generated from the seed, so the same settings always give the same world.</CardDescription></CardHeader>
      <CardContent class="grid gap-4 grid-cols-1">
        <div class="grid gap-4 sm:grid-cols-2 grid-cols-1">
          <div class="grid gap-2 grid-cols-1"><span class="text-sm font-medium">Cover</span><ChoiceGroup ariaLabel="Cover density" layout="segmented" bind:value={coverPerSite} choices={density}/></div>
          <div class="grid gap-2 grid-cols-1"><span class="text-sm font-medium">Loot</span><ChoiceGroup ariaLabel="Loot amount" layout="segmented" bind:value={lootPerSite} choices={loot}/></div>
        </div>
        <details class="group rounded-lg border border-border px-3 py-2">
          <summary class="cursor-pointer text-sm text-muted-foreground select-none group-open:mb-3">Advanced settings</summary>
          <div class="grid gap-4 grid-cols-1">
            {#if large}<div class="grid gap-2 grid-cols-1"><div class="flex justify-between text-sm"><span>Sites</span><span class="font-mono text-primary">{siteCount}</span></div><Slider ariaLabel="Sites" bind:value={siteCount} min={4} max={128} step={4}/></div>{/if}
            <div class="grid gap-2 grid-cols-1"><span class="text-sm">Map seed <span class="text-xs text-muted-foreground">(0 = random on create)</span></span><div class="flex gap-2"><Input type="number" min={0} max={999999} bind:value={seed} ariaLabel="Map seed"/><Button variant="secondary" onclick={onrandomize} disabled={busy}>Randomize</Button></div></div>
            <div class="grid gap-2 grid-cols-1"><span class="text-sm">Exact duration (seconds)</span><Input type="number" min={10} max={2700} bind:value={duration} ariaLabel="Duration in seconds"/></div>
            <label class="m-0 font-sans normal-case tracking-normal flex items-center gap-2 text-sm"><input type="checkbox" class="size-4 accent-primary" bind:checked={liveEdit}/>Allow admin map edits during the match</label>
          </div>
        </details>
      </CardContent>
    </Card>
  </div>

  <div class="grid content-start gap-5 xl:sticky xl:top-4 grid-cols-1">
    <Card>
      <CardHeader class="flex-row items-center justify-between"><CardTitle>Map preview</CardTitle>{#if previewLoading}<Badge variant="muted">Updating…</Badge>{:else if preview}<Badge variant="live">{seed ? `Seed ${seed}` : 'Random seed'}</Badge>{/if}</CardHeader>
      <CardContent>
        {#if preview}<MapPreview {preview}/>{:else}<div class="grid aspect-[16/10] place-items-center rounded-lg border border-dashed border-border text-center text-xs text-muted-foreground grid-cols-1">{signedIn ? 'Preview loads as you change settings.' : 'Sign in to preview the generated map.'}</div>{/if}
      </CardContent>
    </Card>
    <Card>
      <CardHeader><CardTitle>Summary</CardTitle></CardHeader>
      <CardContent class="grid gap-3 grid-cols-1">
        <dl class="m-0 grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted-foreground">Mode</dt><dd class="m-0 text-right">{modes.find(entry => entry.value === mode)?.label}</dd>
          <dt class="text-muted-foreground">Robots</dt><dd class="m-0 text-right font-mono">{mode === 'quick-duel' ? 2 : capacity}</dd>
          <dt class="text-muted-foreground">World</dt><dd class="m-0 text-right font-mono">{world}</dd>
          <dt class="text-muted-foreground">Length</dt><dd class="m-0 text-right font-mono">{Number.isInteger(minutes) ? `${minutes} min` : `${duration} s`}</dd>
          <dt class="text-muted-foreground">Seed</dt><dd class="m-0 text-right font-mono">{seed || 'random'}</dd>
        </dl>
        <Separator/>
        <ol class="m-0 grid gap-1 pl-4 text-xs text-muted-foreground grid-cols-1"><li>Create the lobby.</li><li>Register your saved robot.</li><li>Start once every human robot is connected.</li></ol>
        {#if signedIn}
          <Button size="lg" class="w-full" onclick={oncreate} disabled={busy}>{busy ? 'Creating…' : 'Create lobby'}</Button>
        {:else}
          <Button size="lg" class="w-full" onclick={onSignIn}>Sign in to create a match</Button>
        {/if}
      </CardContent>
    </Card>
  </div>
</div>
