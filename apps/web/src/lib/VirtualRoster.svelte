<script lang="ts">
  import { hpFraction, killsLeader, teamColor, weaponGlyph } from './matchStats';
  import type { RobotState } from './types';
  let { robots, selected, youId = '', onselect }: { robots: RobotState[]; selected: string; youId?: string; onselect: (id: string) => void } = $props();
  let scrollTop = $state(0);
  const leader = $derived(killsLeader(robots));
  const ordered = $derived([...robots].sort((a, b) => Number(b.alive) - Number(a.alive) || (b.kills ?? 0) - (a.kills ?? 0) || b.hp - a.hp));
  const rowHeight = 44, viewport = 264;
  let first = $derived(Math.max(0, Math.floor(scrollTop / rowHeight) - 3));
  let visible = $derived(ordered.slice(first, first + Math.ceil(viewport / rowHeight) + 6));
</script>

<div class="roster" role="region" aria-label="Robot roster" onscroll={(event) => scrollTop = event.currentTarget.scrollTop}>
  <div style:height={`${ordered.length * rowHeight}px`}>
    <div class="rows" style:top={`${first * rowHeight}px`}>
      {#each visible as robot, index (robot.robotId)}
        {@const frac = hpFraction(robot)}
        <button class:active={selected === robot.robotId} class:dead={!robot.alive} onclick={() => onselect(robot.robotId)}>
          <span class="rank">#{first + index + 1}</span>
          <span class="chip" style:background={teamColor(robot.team)}></span>
          <span class="main">
            <span class="name">{robot.robotId === leader?.robotId ? '♛ ' : ''}{robot.name}{robot.robotId === youId ? ' · YOU' : ''}</span>
            <span class="bar"><i style:width={`${frac * 100}%`} style:background={frac > 0.5 ? '#adf08a' : frac > 0.25 ? '#ffc857' : '#ff5b4d'}></i></span>
          </span>
          <span class="meta" title="Weapon">{weaponGlyph(robot.weapon)}</span>
          <span class="meta">{robot.kills ?? 0} K</span>
          <span class="hp">{robot.alive ? `${Math.round(robot.hp)}` : '✕'}</span>
        </button>
      {/each}
    </div>
  </div>
</div>

<style>
  .roster{max-height:264px;overflow:auto;border:1px solid #304d3b}.roster>div{position:relative}.rows{position:absolute;left:0;right:0}
  .rows button{height:44px;width:100%;display:flex;align-items:center;gap:8px;padding:6px 10px;border:0;border-bottom:1px solid #243e2e;background:#13271a;color:#dbe7dc;cursor:pointer;font-size:12px;text-align:left}
  .rows button.active{color:#dfff86;background:#29432d}
  .rows button.dead{opacity:.55}
  .rank{color:#7d917f;font:10px monospace;min-width:28px}
  .chip{width:10px;height:10px;border-radius:50%;flex-shrink:0}
  .main{flex:1;min-width:0;display:grid;gap:3px}
  .name{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
  .bar{height:3px;background:#0a1a12}
  .bar i{display:block;height:100%}
  .meta{color:#9cb8a5;font-size:11px;min-width:22px;text-align:center}
  .hp{font:11px monospace;color:#e5eadf;min-width:30px;text-align:right}
</style>
