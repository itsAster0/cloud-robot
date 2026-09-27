<script lang="ts">
  import { biomeAt } from './terrain';
  import { biomeInfo, hazardInfo, lootColor, lootInfo, materialInfo, siteInfo } from './mapInfo';
  import type { Picked } from './picking';
  import type { WorldSite } from './types';

  // Info card for whatever the viewer clicked that is not a robot.
  let { picked, sites = [], onclose }: { picked: Picked; sites?: WorldSite[]; onclose?: () => void } = $props();
  const at = $derived.by(() => {
    switch (picked.kind) {
      case 'item': return picked.item;
      case 'transit': return picked.transit;
      case 'hazard': return { x: picked.hazard.x + picked.hazard.width / 2, y: picked.hazard.y + picked.hazard.height / 2 };
      case 'obstacle': return { x: picked.obstacle.x + (picked.obstacle.width ?? 0) / 2, y: picked.obstacle.y + (picked.obstacle.height ?? 0) / 2 };
      case 'site': return picked.site;
      default: return picked;
    }
  });
  const biome = $derived(picked.kind === 'site' && picked.site.biome ? picked.site.biome : biomeAt(sites, at.x, at.y));
  const land = $derived(biomeInfo[biome] ?? biomeInfo.forest);
  const kindLabel: Record<Picked['kind'], string> = { item: 'Loot', transit: 'Transit pad', hazard: 'Hazard', obstacle: 'Terrain', site: 'Site', ground: 'Ground' };
  const title = (id: string) => id.replace(/_/g, ' ').replace(/^\w/, c => c.toUpperCase());
</script>

<div class="grid gap-3 rounded-2xl border border-border bg-card p-4 text-sm" aria-live="polite">
  <div class="flex items-start justify-between gap-2">
    <div>
      <span class="text-[11px] tracking-wide text-muted-foreground uppercase">{kindLabel[picked.kind]}</span>
      <h3 class="m-0 text-base font-semibold">
        {#if picked.kind === 'item'}Loot crate
        {:else if picked.kind === 'transit'}Transit pad
        {:else if picked.kind === 'hazard'}{hazardInfo(picked.hazard.kind, biome, picked.hazard.damagePerSecond).name}
        {:else if picked.kind === 'obstacle'}{(materialInfo[picked.obstacle.material ?? 'wall'] ?? materialInfo.wall).name}
        {:else if picked.kind === 'site'}{title(picked.site.kind)}
        {:else}{land.name}{/if}
      </h3>
    </div>
    <button type="button" class="m-0 h-7 cursor-pointer rounded-md border border-border bg-transparent px-2 text-xs text-muted-foreground hover:text-foreground" onclick={onclose} aria-label="Close">✕</button>
  </div>

  {#if picked.kind === 'item'}
    {#if picked.item.contents?.length}
      <ul class="m-0 grid gap-2 p-0">
        {#each picked.item.contents as stack}
          {@const info = lootInfo(stack.kind)}
          <li class="flex list-none items-start gap-2">
            <i class="mt-1 size-3 shrink-0 rounded-sm" style:background={lootColor[info.group]}></i>
            <span><strong>{info.label}</strong>{stack.count > 1 ? ` ×${stack.count}` : ''}<span class="block text-xs text-muted-foreground">{info.text}</span></span>
          </li>
        {/each}
      </ul>
    {:else}<p class="m-0 text-muted-foreground">Contents unknown in this view.</p>{/if}
    <p class="m-0 text-xs text-muted-foreground">Robots pick it up by driving next to it.</p>
  {:else if picked.kind === 'transit'}
    <p class="m-0">Drive onto the pad to jump to <span class="font-mono">({Math.round(picked.transit.targetX)}, {Math.round(picked.transit.targetY)})</span>.</p>
  {:else if picked.kind === 'hazard'}
    <p class="m-0">{hazardInfo(picked.hazard.kind, biome, picked.hazard.damagePerSecond).text}</p>
    <p class="m-0 font-mono text-xs text-muted-foreground">{Math.round(picked.hazard.width)} × {Math.round(picked.hazard.height)} units</p>
  {:else if picked.kind === 'obstacle'}
    <p class="m-0">{(materialInfo[picked.obstacle.material ?? 'wall'] ?? materialInfo.wall).text}</p>
    <p class="m-0 font-mono text-xs text-muted-foreground">{Math.round(picked.obstacle.width ?? 0)} × {Math.round(picked.obstacle.height ?? 0)} units</p>
  {:else if picked.kind === 'site'}
    <p class="m-0">{siteInfo[picked.site.kind] ?? 'A landmark with loot.'}</p>
  {/if}

  <div class="rounded-lg bg-muted/60 p-3">
    <span class="block text-xs text-muted-foreground">Biome · {land.name}</span>
    <span class="text-xs">{land.text}</span>
  </div>
  <p class="m-0 font-mono text-xs text-muted-foreground">x {Math.round(at.x)} · y {Math.round(at.y)}</p>
</div>
