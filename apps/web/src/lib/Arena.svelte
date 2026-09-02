<script lang="ts">
  import { onMount } from 'svelte';
  import type { ArenaItem, RobotState, Snapshot } from './types';
  let { snapshot }: { snapshot: Snapshot | null } = $props();
  let canvas: HTMLCanvasElement;
  let width = $state(800);
  let frame = 0, receivedAt = 0, visible = true;
  let wakeFrame = () => {};
  let previous: Snapshot | null = null, target: Snapshot | null = null;
  let reducedMotion = false;
  let legendOpen = $state(false);
  let explosions: { x: number; y: number; at: number }[] = [];
  let seenExplosions = new Set<string>();
  // Per-snapshot robot indexes replace per-frame array finds.
  let prevRobots = new Map<string, RobotState>(), targetRobots = new Map<string, RobotState>();

  // Canvas state is only touched when something actually changed: reassigning
  // width/height reallocates the backing store, which at 60 fps used to churn
  // hundreds of MB per second and force style/layout recalcs every frame.
  let context: CanvasRenderingContext2D | null = null;
  let canvasKey = '';
  let staticLayer: HTMLCanvasElement | null = null;
  let staticKey = '';
  // Glow sprites replace per-entity shadowBlur, the single most expensive
  // Canvas2D operation; each color+radius variant is rendered exactly once.
  const glowCache = new Map<string, HTMLCanvasElement>();
  function glow(color: string, radius: number) {
    const key = `${color}:${radius}`;
    let sprite = glowCache.get(key);
    if (!sprite) {
      sprite = document.createElement('canvas');
      sprite.width = radius * 2;
      sprite.height = radius * 2;
      const g = sprite.getContext('2d');
      if (g) {
        const gradient = g.createRadialGradient(radius, radius, 0, radius, radius, radius);
        gradient.addColorStop(0, color);
        gradient.addColorStop(1, `${color}00`);
        g.fillStyle = gradient;
        g.fillRect(0, 0, radius * 2, radius * 2);
      }
      glowCache.set(key, sprite);
    }
    return sprite;
  }
  // Quantized alpha strings keep per-frame string building out of the hot loop.
  const minePulses = Array.from({ length: 21 }, (_, i) => `rgba(255,91,77,${(0.35 * (i / 20)).toFixed(3)})`);
  const boomRings = Array.from({ length: 21 }, (_, i) => `rgba(255,140,77,${(1 - i / 20).toFixed(3)})`);
  const boomAge = (now: number, at: number) => Math.max(0, Math.min(20, Math.round(((now - at) / 480) * 20)));

  function robotPosition(robot: RobotState, now: number) {
    if (reducedMotion || !previous) return robot;
    const before = prevRobots.get(robot.robotId);
    if (!before) return robot;
    const amount = Math.min(1, (now - receivedAt) / 100);
    return {
      x: before.x + (robot.x - before.x) * amount,
      y: before.y + (robot.y - before.y) * amount,
      heading: before.heading + (robot.heading - before.heading) * amount,
    };
  }

  // Solo free-for-all matches assign every robot its own team, so non-red and
  // non-blue teams hash into a stable palette entry.
  const teamPalette = ['#dfff86', '#ffc857', '#ff8fd0', '#54ffd0', '#c78bff', '#f5f5b0', '#ffa76b', '#7bf1ff'];
  function teamColor(team: string) {
    if (team === 'red') return '#ff5b4d';
    if (team === 'blue') return '#54a7ff';
    let hash = 0;
    for (let index = 0; index < team.length; index++) hash = (hash * 31 + team.charCodeAt(index)) >>> 0;
    return teamPalette[hash % teamPalette.length];
  }
  function teamGlyph(robot: RobotState) {
    if (robot.team === 'red') return 'R';
    if (robot.team === 'blue') return 'B';
    return robot.name.slice(0, 1).toUpperCase();
  }

  // Protocol v3 loot palette: healing greens, teal medkits, blue defense,
  // amber utility, purple epic boosts, red weapon drops. Unlisted types fall
  // back to the historic amber.
  const itemColors: Record<string, string> = {
    heal: '#b9f542', 'repair-core': '#b9f542', repair_core: '#b9f542',
    medkit: '#3fe0c5', nano_repair: '#3fe0c5',
    shield: '#54a7ff', armor_plate: '#54a7ff',
    battery: '#ffc857', scanner: '#ffc857', dash_cell: '#ffc857', overdrive: '#ffc857', rapid_fire: '#ffc857',
    cloak: '#c78bff', berserker_charm: '#c78bff', vampiric_fang: '#c78bff', frenzy: '#c78bff', teleport_beacon: '#c78bff',
  };
  const weaponItems = new Set(['machine_gun', 'incendiary', 'cryo', 'emp']);
  function itemColor(type: string) {
    return itemColors[type] ?? (type.startsWith('weapon_') || weaponItems.has(type) ? '#ff5b4d' : '#ffc857');
  }
  // Engine rarity order, used when a snapshot omits the per-item field
  // (replay hydrations, older gateways).
  const epicItems = new Set(['weapon_railgun', 'weapon_grenade', 'weapon_mine_layer', 'cloak', 'teleport_beacon', 'berserker_charm', 'vampiric_fang', 'frenzy']);
  const rareItems = new Set(['shield', 'overdrive', 'rapid_fire', 'medkit', 'nano_repair', 'armor_plate', 'scanner', 'dash_cell', 'weapon_shotgun']);
  function itemRarity(item: ArenaItem) {
    return item.rarity ?? (epicItems.has(item.type) ? 'epic' : rareItems.has(item.type) ? 'rare' : 'common');
  }

  const legendRows = [
    { rarity: 'common', entries: ['heal', 'repair-core', 'battery', 'weapon_cannon', 'machine_gun', 'incendiary', 'cryo', 'emp'] },
    { rarity: 'rare', entries: ['medkit', 'nano_repair', 'shield', 'armor_plate', 'overdrive', 'rapid_fire', 'scanner', 'dash_cell', 'weapon_shotgun'] },
    { rarity: 'epic', entries: ['cloak', 'teleport_beacon', 'berserker_charm', 'vampiric_fang', 'frenzy', 'weapon_grenade', 'weapon_railgun', 'weapon_mine_layer'] },
  ];
  function legendColor(type: string) {
    const color = itemColor(type);
    return color === '#b9f542' ? 'green' : color === '#3fe0c5' ? 'teal' : color === '#54a7ff' ? 'blue' : color === '#c78bff' ? 'purple' : color === '#ff5b4d' ? 'red' : 'amber';
  }

  // Explosion rings are ephemeral client effects: each arena event fires once,
  // keyed by sequence + tick + position, and fades over half a second.
  function recordExplosions(snap: Snapshot) {
    for (const event of snap.events ?? []) {
      if (event.type !== 'explosion' && event.type !== 'mine_exploded') continue;
      const robot = event.x == null || event.y == null ? targetRobots.get(event.targetId ?? event.robotId ?? '') : null;
      const x = event.x ?? robot?.x, y = event.y ?? robot?.y;
      if (x == null || y == null) continue;
      const key = `${snap.sequence}:${event.tick ?? snap.tick}:${x}:${y}`;
      if (seenExplosions.has(key)) continue;
      seenExplosions.add(key);
      explosions.push({ x, y, at: performance.now() });
    }
    if (seenExplosions.size > 400) seenExplosions = new Set([...seenExplosions].slice(-200));
  }

  // prepareCanvas only reallocates the backing store when the viewport, DPR,
  // or world size actually changed — canvas.width assignment wipes the bitmap
  // and used to run (and reallocate ~8 MB) on every animation frame.
  function prepareCanvas(heightPx: number, ratio: number) {
    const key = `${Math.round(width)}:${ratio}:${target?.width ?? 800}:${target?.height ?? 500}`;
    if (key === canvasKey && context) return;
    canvasKey = key;
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(heightPx * ratio);
    canvas.style.height = `${heightPx}px`;
    context = canvas.getContext('2d');
    if (context) context.scale(ratio, ratio);
    staticKey = '';
  }

  // buildStatic caches the per-match layers that never animate (background,
  // grid, obstacles) into an offscreen canvas blitted every frame.
  function buildStatic(heightPx: number, ratio: number, scale: number): HTMLCanvasElement | null {
    const key = `${canvasKey}:${target?.matchId ?? ''}:${target?.mapId ?? ''}`;
    if (key === staticKey && staticLayer) return staticLayer;
    const worldWidth = target?.width ?? 800, worldHeight = target?.height ?? 500;
    const layer = document.createElement('canvas');
    layer.width = Math.round(width * ratio);
    layer.height = Math.round(heightPx * ratio);
    const g = layer.getContext('2d');
    if (!g) return null;
    g.scale(ratio, ratio);
    g.fillStyle = '#06100d';
    g.fillRect(0, 0, width, heightPx);
    g.strokeStyle = 'rgba(61,96,83,.25)';
    g.lineWidth = 1;
    for (let x = 0; x <= worldWidth; x += 50) { g.beginPath(); g.moveTo(x * scale, 0); g.lineTo(x * scale, heightPx); g.stroke(); }
    for (let y = 0; y <= worldHeight; y += 50) { g.beginPath(); g.moveTo(0, y * scale); g.lineTo(width, y * scale); g.stroke(); }
    for (const obstacle of target?.obstacles ?? []) {
      g.fillStyle = '#182c25'; g.strokeStyle = '#496057'; g.lineWidth = 2; g.beginPath();
      if (obstacle.shape === 'circle') g.arc(obstacle.x * scale, obstacle.y * scale, (obstacle.radius ?? 20) * scale, 0, Math.PI * 2);
      else g.rect(obstacle.x * scale, obstacle.y * scale, (obstacle.width ?? 40) * scale, (obstacle.height ?? 40) * scale);
      g.fill(); g.stroke();
    }
    staticLayer = layer;
    staticKey = key;
    return layer;
  }

  function draw(now: number) {
    if (!canvas) return;
    // A 3x/4x backing store costs 2.25x/4x more pixel work than 2x with no
    // useful improvement for this small tactical view.
    const ratio = Math.min(window.devicePixelRatio || 1, 2), worldWidth = target?.width ?? 800, worldHeight = target?.height ?? 500;
    const height = width * worldHeight / worldWidth;
    prepareCanvas(height, ratio);
    const drawContext = context;
    if (!drawContext) return;
    const scale = width / worldWidth;
    const layer = buildStatic(height, ratio, scale);
    if (layer) drawContext.drawImage(layer, 0, 0, width, height);
    else { drawContext.fillStyle = '#06100d'; drawContext.fillRect(0, 0, width, height); }
    if (!target) { drawContext.fillStyle = '#718179'; drawContext.font = '11px DM Mono'; drawContext.textAlign = 'center'; drawContext.fillText('ARENA WAITING FOR MATCH START', width / 2, height / 2); return; }
    const zone = target.zone;
    if (zone) {
      const zoneX = zone.x * scale, zoneY = zone.y * scale, zoneRadius = Math.max(0, zone.radius) * scale;
      if (zone.active) {
        drawContext.fillStyle = 'rgba(255,91,77,.07)';
        drawContext.beginPath(); drawContext.rect(0, 0, width, height); drawContext.arc(zoneX, zoneY, zoneRadius, 0, Math.PI * 2, true); drawContext.fill('evenodd');
      }
      drawContext.strokeStyle = zone.active ? 'rgba(255,91,77,.55)' : 'rgba(255,91,77,.25)'; drawContext.lineWidth = 1.5; drawContext.setLineDash([6, 6]);
      drawContext.beginPath(); drawContext.arc(zoneX, zoneY, zoneRadius, 0, Math.PI * 2); drawContext.stroke(); drawContext.setLineDash([]);
    }
    for (const item of target.items ?? []) {
      if (!item.active) continue;
      const x = item.x * scale, y = item.y * scale, half = 7 * scale;
      const color = itemColor(item.type), rarity = itemRarity(item);
      if (rarity !== 'common') {
        const halo = glow(color, rarity === 'epic' ? 18 : 11);
        const size = (rarity === 'epic' ? 44 : 28) * scale;
        drawContext.drawImage(halo, x - size / 2, y - size / 2, size, size);
      }
      drawContext.fillStyle = color; drawContext.strokeStyle = '#07110f'; drawContext.lineWidth = 2;
      drawContext.fillRect(x - half, y - 2 * scale, half * 2, 4 * scale); drawContext.fillRect(x - 2 * scale, y - half, 4 * scale, half * 2);
      drawContext.strokeRect(x - 8 * scale, y - 8 * scale, 16 * scale, 16 * scale);
      if (rarity === 'rare') { drawContext.strokeStyle = color; drawContext.globalAlpha = .55; drawContext.lineWidth = 1; drawContext.strokeRect(x - 10.5 * scale, y - 10.5 * scale, 21 * scale, 21 * scale); drawContext.globalAlpha = 1; }
      if (rarity === 'epic') { drawContext.strokeStyle = color; drawContext.lineWidth = 1.5; drawContext.beginPath(); drawContext.arc(x, y, 13 * scale, 0, Math.PI * 2); drawContext.stroke(); }
    }
    for (const mine of target.mines ?? []) {
      if (!mine.active) continue;
      const x = mine.x * scale, y = mine.y * scale;
      const armed = target.tick >= (mine.armTick ?? mine.spawnTick + 30);
      const pulse = armed && !reducedMotion ? .5 + .5 * Math.sin(now / 180) : 0;
      drawContext.save(); drawContext.globalAlpha = armed ? 1 : .38;
      drawContext.fillStyle = armed ? '#ff5b4d' : '#7d2320'; drawContext.strokeStyle = '#07110f'; drawContext.lineWidth = 1.5;
      drawContext.beginPath(); drawContext.moveTo(x, y - 7 * scale); drawContext.lineTo(x + 6 * scale, y + 5 * scale); drawContext.lineTo(x - 6 * scale, y + 5 * scale); drawContext.closePath(); drawContext.fill(); drawContext.stroke();
      drawContext.restore();
      if (pulse > 0) { drawContext.strokeStyle = minePulses[Math.round(pulse * 20)]; drawContext.lineWidth = 1.5; drawContext.beginPath(); drawContext.arc(x, y, (9 + 3 * pulse) * scale, 0, Math.PI * 2); drawContext.stroke(); }
    }
    for (const turret of target.turrets ?? []) {
      const x = turret.x * scale, y = turret.y * scale, size = 7 * scale;
      if (!turret.alive) { drawContext.strokeStyle = '#2c3a34'; drawContext.lineWidth = 1.5; drawContext.strokeRect(x - size, y - size, size * 2, size * 2); continue; }
      drawContext.fillStyle = '#41504a'; drawContext.strokeStyle = '#5c6f66'; drawContext.lineWidth = 1.5;
      drawContext.fillRect(x - size, y - size, size * 2, size * 2); drawContext.strokeRect(x - size, y - size, size * 2, size * 2);
      const share = Math.max(0, Math.min(1, turret.maxHp > 0 ? turret.hp / turret.maxHp : 0));
      if (share > 0) { drawContext.strokeStyle = share > .5 ? '#b9f542' : '#ffc857'; drawContext.lineWidth = 2; drawContext.beginPath(); drawContext.arc(x, y, size + 4 * scale, -Math.PI / 2, -Math.PI / 2 + Math.PI * 2 * share); drawContext.stroke(); }
    }
    for (const projectile of target.projectiles ?? []) {
      const x = projectile.x * scale, y = projectile.y * scale;
      const teamColor = projectile.team === 'red' ? 'rgba(255,91,77,.45)' : projectile.team === 'blue' ? 'rgba(84,167,255,.45)' : 'rgba(223,255,134,.45)';
      drawContext.strokeStyle = teamColor; drawContext.lineWidth = 2 * scale; drawContext.beginPath(); drawContext.moveTo((projectile.x - projectile.vx * .7) * scale, (projectile.y - projectile.vy * .7) * scale); drawContext.lineTo(x, y); drawContext.stroke();
      const halo = glow('#b9f542', 10), haloSize = 20 * scale;
      drawContext.drawImage(halo, x - haloSize / 2, y - haloSize / 2, haloSize, haloSize);
      drawContext.fillStyle = '#dfff86'; drawContext.beginPath(); drawContext.arc(x, y, 4 * scale, 0, Math.PI * 2); drawContext.fill();
    }
    // Radar rings are drawn for every living robot before the bodies so the
    // triangles and labels stay on top of the overlap. visionRange is the
    // engine's per-tick perception radius (optics/scanner widen it).
    for (const robot of target.robots) {
      if (!robot.alive || !robot.visionRange) continue;
      const position = robotPosition(robot, now);
      drawContext.strokeStyle = teamColor(robot.team);
      drawContext.globalAlpha = .16;
      drawContext.lineWidth = 1;
      drawContext.setLineDash([4, 7]);
      drawContext.beginPath();
      drawContext.arc(position.x * scale, position.y * scale, robot.visionRange * scale, 0, Math.PI * 2);
      drawContext.stroke();
      drawContext.setLineDash([]);
      drawContext.globalAlpha = 1;
    }
    for (const robot of target.robots) {
      const position = robotPosition(robot, now), x = position.x * scale, y = position.y * scale;
      const color = teamColor(robot.team);
      drawContext.save(); drawContext.translate(x, y); drawContext.rotate(position.heading * Math.PI / 180); drawContext.fillStyle = robot.alive ? color : '#34423c'; drawContext.strokeStyle = '#07110f'; drawContext.lineWidth = 3; drawContext.beginPath(); drawContext.moveTo(17 * scale, 0); drawContext.lineTo(-12 * scale, -12 * scale); drawContext.lineTo(-8 * scale, 0); drawContext.lineTo(-12 * scale, 12 * scale); drawContext.closePath(); drawContext.fill(); drawContext.stroke(); drawContext.fillStyle = '#07110f'; drawContext.font = '9px DM Mono'; drawContext.textAlign = 'center'; drawContext.fillText(teamGlyph(robot), 0, 3 * scale); drawContext.restore();
      drawContext.fillStyle = '#0b1814'; drawContext.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale, 4 * scale); drawContext.fillStyle = color; drawContext.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale * robot.hp / 100, 4 * scale); drawContext.fillStyle = '#dfe9e4'; drawContext.font = '9px DM Mono'; drawContext.textAlign = 'center'; drawContext.fillText(robot.name, x, y + 30 * scale);
    }
    if (explosions.length) explosions = explosions.filter((boom) => now - boom.at < 480);
    for (const boom of explosions) {
      const step = boomAge(now, boom.at), age = step / 20;
      const radius = (8 + 44 * (reducedMotion ? .5 : age)) * scale;
      drawContext.strokeStyle = boomRings[20 - step]; drawContext.lineWidth = 2.5 * (1 - age) + .5;
      drawContext.beginPath(); drawContext.arc(boom.x * scale, boom.y * scale, radius, 0, Math.PI * 2); drawContext.stroke();
    }
    for (const event of target.events ?? []) {
      if (event.type !== 'hit' || !event.damage) continue;
      const victim = targetRobots.get(event.targetId ?? '');
      if (!victim) continue;
      drawContext.fillStyle = '#fff1ad'; drawContext.font = '600 12px DM Mono'; drawContext.textAlign = 'center'; drawContext.fillText(`-${event.damage}`, victim.x * scale, victim.y * scale - 35 * scale); drawContext.strokeStyle = '#fff1ad'; drawContext.beginPath(); drawContext.arc(victim.x * scale, victim.y * scale, 23 * scale, 0, Math.PI * 2); drawContext.stroke();
    }
    if (zone?.stage != null) {
      const final = zone.stage >= 3;
      drawContext.fillStyle = final ? '#ff5b4d' : '#ffc857'; drawContext.font = '600 11px DM Mono'; drawContext.textAlign = 'right';
      drawContext.fillText(final ? `FINAL ZONE · STAGE ${zone.stage}/3` : `ZONE STAGE ${zone.stage}/3`, width - 12, 20);
    }
  }

  function needsAnotherFrame(now: number) {
    if (target?.status === 'running') return true;
    if (previous && now - receivedAt < 100) return true;
    return explosions.some((boom) => now - boom.at < 480);
  }

  onMount(() => {
    reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches || localStorage.getItem('arena-reduced-motion') === 'true';
    const requestFrame = () => {
      if (!visible || frame) return;
      frame = requestAnimationFrame((now) => {
        frame = 0;
        draw(now);
        if (needsAnotherFrame(now)) requestFrame();
      });
    };
    wakeFrame = requestFrame;
    const observer = new ResizeObserver(([entry]) => { width = entry.contentRect.width; requestFrame(); });
    const visibilityObserver = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
      if (visible) requestFrame();
      else if (frame) { cancelAnimationFrame(frame); frame = 0; }
    });
    observer.observe(canvas.parentElement!);
    visibilityObserver.observe(canvas);
    requestFrame();
    return () => { wakeFrame = () => {}; observer.disconnect(); visibilityObserver.disconnect(); if (frame) cancelAnimationFrame(frame); };
  });
  $effect(() => {
    if (!snapshot || snapshot === target) return;
    if (!previous || previous.matchId !== snapshot.matchId) { seenExplosions.clear(); explosions = []; }
    previous = target;
    if (previous) prevRobots = new Map(previous.robots.map((robot) => [robot.robotId, robot]));
    target = snapshot;
    targetRobots = new Map(target.robots.map((robot) => [robot.robotId, robot]));
    receivedAt = performance.now();
    recordExplosions(snapshot);
    wakeFrame();
  });
</script>
<canvas bind:this={canvas} aria-label="Live robot arena. Team glyphs and per-team colors distinguish robots; color is not the only identifier. Mines, turrets, and the collapsing zone are drawn in place."></canvas>
<div class="arena-legend">
  <button class="legend-toggle" onclick={() => (legendOpen = !legendOpen)} aria-expanded={legendOpen}>{legendOpen ? 'HIDE LEGEND' : 'ITEMS + HAZARDS'}</button>
  {#if legendOpen}
    <div class="legend-panel">
      {#each legendRows as row (row.rarity)}
        <div class="legend-row"><span class="legend-tag" data-rarity={row.rarity}>{row.rarity.toUpperCase()}</span>{#each row.entries as entry (entry)}<i class="sw" data-c={legendColor(entry)} data-r={row.rarity}></i><span>{entry}</span>{/each}</div>
      {/each}
      <div class="legend-row legend-hazards"><i class="hz-mine"></i><span>mine</span><i class="hz-turret"></i><span>turret</span><i class="hz-zone"></i><span>zone</span><i class="hz-vision"></i><span>radar</span><em>epic loot glows brightest</em></div>
    </div>
  {/if}
</div>
