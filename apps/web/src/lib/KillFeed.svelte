<script lang="ts">
  import { teamColor } from './matchStats';
  import type { ArenaEvent, RobotState } from './types';

  // Recent kills, bounties, joins, and Uplink moves, newest first, laid over
  // the top-left of the map. Entries fade after a few seconds.
  let { events, robots }: { events: (ArenaEvent & { seen: number })[]; robots: RobotState[] } = $props();
  const byId = $derived(new Map(robots.map(r => [r.robotId, r])));
  const name = (id?: string) => (id && byId.get(id)?.name) || id?.slice(0, 8) || '?';
  const color = (id?: string) => { const r = id ? byId.get(id) : undefined; return r ? teamColor(r.team) : '#c9d6cb'; };
</script>

<ol class="feed" aria-live="polite" aria-label="Kill feed">
  {#each events as e (`${e.tick}:${e.type}:${e.robotId}:${e.targetId}`)}
    <li class:bounty={e.type === 'bounty_claimed'}>
      {#if e.type === 'kill'}
        <b style:color={color(e.robotId)}>{name(e.robotId)}</b><span class="icon">⌖</span><b style:color={color(e.targetId)}>{name(e.targetId)}</b>
      {:else if e.type === 'bounty_claimed'}
        <b style:color={color(e.robotId)}>{name(e.robotId)}</b> claimed <b>${e.damage}</b> bounty on <b style:color={color(e.targetId)}>{name(e.targetId)}</b>
      {:else if e.type === 'hill_moved'}
        <span class="icon">⬡</span> Uplink moved to {e.message?.replace('site-', 'site ')}
      {:else if e.type === 'zone_moved'}
        <span class="icon">◎</span> The safe zone is moving: follow the dashed ring
      {:else if e.type === 'reinforcements'}
        <span class="icon">✚</span> {e.message} bot reinforcements dropped in
      {:else if e.type === 'robot_joined'}
        <b style:color={color(e.robotId)}>{name(e.robotId)}</b> joined the arena
      {/if}
    </li>
  {/each}
</ol>

<style>
  .feed { position:absolute; left:12px; top:60px; z-index:2; margin:0; padding:0; display:grid; gap:4px; max-width:min(360px, 60%); pointer-events:none; }
  li { list-style:none; font:12px var(--font-mono, monospace); color:#e5eadf; background:rgba(4,10,8,.72); border:1px solid rgba(255,255,255,.08); border-radius:6px; padding:4px 8px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; animation:in .25s ease both; }
  li.bounty { border-color:#ffc85780; background:rgba(40,28,4,.8); }
  .icon { margin:0 6px; color:#ff8a6a; }
  @keyframes in { from { opacity:0; transform:translateX(-8px); } }
  @media (prefers-reduced-motion: reduce) { li { animation:none; } }
</style>
