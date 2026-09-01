<script lang="ts">
  import { onMount } from 'svelte';
  import type { RobotState, Snapshot } from './types';
  let { snapshot }: { snapshot: Snapshot | null } = $props();
  let canvas: HTMLCanvasElement;
  let width = $state(800);
  let frame = 0, receivedAt = 0;
  let previous: Snapshot | null = null, target: Snapshot | null = null;
  let reducedMotion = false;

  function robotAt(robot: RobotState, now: number) {
    if (reducedMotion || !previous) return robot;
    const before = previous.robots.find((entry) => entry.robotId === robot.robotId);
    if (!before) return robot;
    const amount = Math.min(1, (now - receivedAt) / 100);
    return { ...robot, x: before.x + (robot.x - before.x) * amount, y: before.y + (robot.y - before.y) * amount, heading: before.heading + (robot.heading - before.heading) * amount };
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

  function draw(now: number) {
    if (!canvas) return;
    const ratio = window.devicePixelRatio || 1, worldWidth = target?.width ?? 800, worldHeight = target?.height ?? 500;
    const height = width * worldHeight / worldWidth;
    canvas.width = Math.round(width * ratio); canvas.height = Math.round(height * ratio); canvas.style.height = `${height}px`;
    const context = canvas.getContext('2d'); if (!context) return;
    context.scale(ratio, ratio); const scale = width / worldWidth;
    context.fillStyle = '#06100d'; context.fillRect(0, 0, width, height); context.strokeStyle = 'rgba(61,96,83,.25)'; context.lineWidth = 1;
    for (let x = 0; x <= worldWidth; x += 50) { context.beginPath(); context.moveTo(x * scale, 0); context.lineTo(x * scale, height); context.stroke(); }
    for (let y = 0; y <= worldHeight; y += 50) { context.beginPath(); context.moveTo(0, y * scale); context.lineTo(width, y * scale); context.stroke(); }
    if (!target) { context.fillStyle = '#718179'; context.font = '11px DM Mono'; context.textAlign = 'center'; context.fillText('ARENA WAITING FOR MATCH START', width / 2, height / 2); return; }
    for (const obstacle of target.obstacles ?? []) {
      context.fillStyle = '#182c25'; context.strokeStyle = '#496057'; context.lineWidth = 2; context.beginPath();
      if (obstacle.shape === 'circle') context.arc(obstacle.x * scale, obstacle.y * scale, (obstacle.radius ?? 20) * scale, 0, Math.PI * 2);
      else context.rect(obstacle.x * scale, obstacle.y * scale, (obstacle.width ?? 40) * scale, (obstacle.height ?? 40) * scale);
      context.fill(); context.stroke();
    }
    for (const item of target.items ?? []) {
      if (!item.active) continue; const x = item.x * scale, y = item.y * scale;
      context.fillStyle = item.type.includes('heal') || item.type.includes('repair') ? '#b9f542' : '#ffc857'; context.strokeStyle = '#07110f'; context.lineWidth = 2;
      context.fillRect(x - 7 * scale, y - 2 * scale, 14 * scale, 4 * scale); context.fillRect(x - 2 * scale, y - 7 * scale, 4 * scale, 14 * scale); context.strokeRect(x - 8 * scale, y - 8 * scale, 16 * scale, 16 * scale);
    }
    for (const projectile of target.projectiles ?? []) {
      const x = projectile.x * scale, y = projectile.y * scale;
      const teamColor = projectile.team === 'red' ? 'rgba(255,91,77,.45)' : projectile.team === 'blue' ? 'rgba(84,167,255,.45)' : 'rgba(223,255,134,.45)';
      context.strokeStyle = teamColor; context.lineWidth = 2 * scale; context.beginPath(); context.moveTo((projectile.x - projectile.vx * .7) * scale, (projectile.y - projectile.vy * .7) * scale); context.lineTo(x, y); context.stroke(); context.fillStyle = '#dfff86'; context.shadowColor = '#b9f542'; context.shadowBlur = 10; context.beginPath(); context.arc(x, y, 4 * scale, 0, Math.PI * 2); context.fill(); context.shadowBlur = 0;
    }
    for (const raw of target.robots) {
      const robot = robotAt(raw, now), x = robot.x * scale, y = robot.y * scale;
      const color = teamColor(robot.team);
      context.save(); context.translate(x, y); context.rotate(robot.heading * Math.PI / 180); context.fillStyle = robot.alive ? color : '#34423c'; context.strokeStyle = '#07110f'; context.lineWidth = 3; context.beginPath(); context.moveTo(17 * scale, 0); context.lineTo(-12 * scale, -12 * scale); context.lineTo(-8 * scale, 0); context.lineTo(-12 * scale, 12 * scale); context.closePath(); context.fill(); context.stroke(); context.fillStyle = '#07110f'; context.font = `${Math.max(8, 9 * scale)}px DM Mono`; context.textAlign = 'center'; context.fillText(teamGlyph(robot), 0, 3 * scale); context.restore();
      context.fillStyle = '#0b1814'; context.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale, 4 * scale); context.fillStyle = color; context.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale * robot.hp / 100, 4 * scale); context.fillStyle = '#dfe9e4'; context.font = `${Math.max(8, 9 * scale)}px DM Mono`; context.textAlign = 'center'; context.fillText(robot.name, x, y + 30 * scale);
    }
    for (const event of target.events ?? []) {
      if (event.type !== 'hit' || !event.damage) continue; const victim = target.robots.find((robot) => robot.robotId === event.targetId); if (!victim) continue;
      context.fillStyle = '#fff1ad'; context.font = `600 ${Math.max(10, 12 * scale)}px DM Mono`; context.textAlign = 'center'; context.fillText(`-${event.damage}`, victim.x * scale, victim.y * scale - 35 * scale); context.strokeStyle = '#fff1ad'; context.beginPath(); context.arc(victim.x * scale, victim.y * scale, 23 * scale, 0, Math.PI * 2); context.stroke();
    }
  }
  onMount(() => {
    reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches || localStorage.getItem('arena-reduced-motion') === 'true';
    const observer = new ResizeObserver(([entry]) => { width = entry.contentRect.width; }); observer.observe(canvas.parentElement!);
    const render = (now: number) => { draw(now); frame = requestAnimationFrame(render); }; frame = requestAnimationFrame(render);
    return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  });
  $effect(() => { if (!snapshot || snapshot === target) return; previous = target; target = snapshot; receivedAt = performance.now(); });
</script>
<canvas bind:this={canvas} aria-label="Live robot arena. Team glyphs and per-team colors distinguish robots; color is not the only identifier."></canvas>
