<script lang="ts">
  import { aliveRobots, elapsedLabel, killsLeader, teamColor, teamStandings, weaponGlyph, weaponLabel } from './matchStats';
  import type { Snapshot, RobotState } from './types';

  // `roster` is the full-match robot list. Public snapshots carry only robots
  // near the viewer's camera, so counting those undercounts the match.
  let { snapshot, roster = [], youId = '' }: { snapshot: Snapshot | null; roster?: RobotState[]; youId?: string } = $props();
  const robots = $derived(roster.length ? roster : snapshot?.robots ?? []);
  const alive = $derived(aliveRobots(robots));
  const arena = $derived(snapshot?.mode === 'arena');
  const leader = $derived(arena ? robots.reduce<RobotState | null>((best, r) => (r.score ?? 0) > (best?.score ?? 0) ? r : best, null) : killsLeader(robots));
  const standings = $derived(teamStandings(robots).slice(0, 4));
  const you: RobotState | undefined = $derived(robots.find((r) => r.robotId === youId));
  const zone = $derived(snapshot?.zone);
</script>

{#if snapshot}
  <div class="overview" role="status" aria-label="Match overview">
    <div class="card">
      <span>Alive</span>
      <strong>{alive.length}<small>/{robots.length}</small></strong>
      <div class="bar"><i style:width={`${robots.length ? (alive.length / robots.length) * 100 : 0}%`}></i></div>
    </div>
    <div class="card">
      <span>{arena ? 'Top score' : 'Kill leader'}</span>
      {#if leader}
        <strong class="leader">♛ {leader.name}</strong>
        <small><i class="dot" style:background={teamColor(leader.team)}></i>{leader.team.slice(0, 8)} · {arena ? `${leader.score ?? 0} pts · ` : ''}{leader.kills} kills · {weaponGlyph(leader.weapon)} {weaponLabel(leader.weapon)}</small>
      {:else}
        <strong>—</strong><small>No kills yet</small>
      {/if}
    </div>
    <div class="card">
      <span>{snapshot.mode === 'arena' ? 'Session' : 'Zone'} · {elapsedLabel(snapshot.tick, snapshot.tickRate ?? 20)}</span>
      {#if snapshot.mode === 'arena'}
        <strong>Open arena</strong>
        <small>No zone · robots respawn</small>
      {:else if zone}
        <strong>{zone.active ? `Stage ${zone.stage || 1}` : 'Stable'}</strong>
        <small>{zone.active ? `◉ ${Math.round(zone.radius)}u · ${zone.damage}/s` : 'Collapse pending'}</small>
      {:else}
        <strong>—</strong>
      {/if}
    </div>
    <div class="card">
      <span>{you ? 'Your robot' : 'Tracked'}</span>
      {#if you}
        <strong>{you.name}</strong>
        <small>{you.alive ? `${Math.round(you.hp)}/${you.maxHp ?? 100} HP · 🛡 ${Math.round(you.shield ?? 0)} · ⚡${Math.round(you.energy ?? 0)} · ${weaponGlyph(you.weapon)}` : snapshot.mode === 'arena' && you.respawnIn != null ? `Respawning in ${Math.ceil(you.respawnIn / (snapshot.tickRate ?? 20))}s` : 'Destroyed · spectating'}</small>
      {:else}
        <strong>—</strong><small>Select a robot below</small>
      {/if}
    </div>
  </div>
  {#if standings.length > 1}
    <div class="standings">
      {#each standings as t}
        <span class="team"><i class="dot" style:background={teamColor(t.team)}></i>{t.team} <b>{t.alive}/{t.total}</b> · {t.kills} kills</span>
      {/each}
    </div>
  {/if}
{/if}

<style>
  .overview { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin: 18px 0 0; }
  .card { padding: 14px; background: #13271a; border: 1px solid #304d3b; }
  .card span { display: block; font-size: 11px; color: #a6b8a6; }
  .card strong { display: block; margin-top: 4px; font-size: 17px; }
  .card strong small, .card small { display: block; margin-top: 4px; font-size: 11px; color: #a6b8a6; font-weight: normal; }
  .card strong.leader { color: #dfff86; }
  .bar { height: 4px; margin-top: 8px; background: #0a1a12; }
  .bar i { display: block; height: 100%; background: #dfff86; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 5px; }
  .standings { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; font: 11px monospace; color: #a6b8a6; }
  .standings .team b { color: #e5eadf; }
  @media (max-width: 720px) { .overview { grid-template-columns: repeat(2, 1fr); } }
</style>
