<script lang="ts">
  import { onMount } from 'svelte';
  import type { Snapshot } from './types';

  let { snapshot }: { snapshot: Snapshot | null } = $props();
  let canvas: HTMLCanvasElement;
  let width = $state(800);

  function draw() {
    if (!canvas) return;
    const ratio = window.devicePixelRatio || 1;
    const height = width * (500 / 800);
    canvas.width = width * ratio;
    canvas.height = height * ratio;
    canvas.style.height = `${height}px`;
    const context = canvas.getContext('2d');
    if (!context) return;
    context.scale(ratio, ratio);
    const scale = width / 800;

    context.fillStyle = '#06100d';
    context.fillRect(0, 0, width, height);
    context.strokeStyle = 'rgba(61, 96, 83, .25)';
    context.lineWidth = 1;
    for (let x = 0; x <= 800; x += 50) {
      context.beginPath(); context.moveTo(x * scale, 0); context.lineTo(x * scale, height); context.stroke();
    }
    for (let y = 0; y <= 500; y += 50) {
      context.beginPath(); context.moveTo(0, y * scale); context.lineTo(width, y * scale); context.stroke();
    }

    if (!snapshot) {
      context.fillStyle = '#718179';
      context.font = '11px DM Mono';
      context.textAlign = 'center';
      context.fillText('ARENA WAITING FOR MATCH START', width / 2, height / 2);
      return;
    }

    for (const projectile of snapshot.projectiles ?? []) {
      const x = projectile.x * scale;
      const y = projectile.y * scale;
      context.strokeStyle = projectile.team === 'red' ? 'rgba(255,91,77,.45)' : 'rgba(84,167,255,.45)';
      context.lineWidth = 2 * scale;
      context.beginPath();
      context.moveTo((projectile.x - projectile.vx * 0.7) * scale, (projectile.y - projectile.vy * 0.7) * scale);
      context.lineTo(x, y);
      context.stroke();
      context.fillStyle = '#dfff86';
      context.shadowColor = '#b9f542';
      context.shadowBlur = 10;
      context.beginPath(); context.arc(x, y, 4 * scale, 0, Math.PI * 2); context.fill();
      context.shadowBlur = 0;
    }

    for (const robot of snapshot.robots) {
      const x = robot.x * scale;
      const y = robot.y * scale;
      context.save();
      context.translate(x, y);
      context.rotate((robot.heading * Math.PI) / 180);
      context.fillStyle = robot.alive ? (robot.team === 'red' ? '#ff5b4d' : '#54a7ff') : '#34423c';
      context.strokeStyle = '#07110f';
      context.lineWidth = 3;
      context.beginPath();
      context.moveTo(17 * scale, 0);
      context.lineTo(-12 * scale, -12 * scale);
      context.lineTo(-8 * scale, 0);
      context.lineTo(-12 * scale, 12 * scale);
      context.closePath();
      context.fill(); context.stroke();
      context.restore();

      context.fillStyle = '#0b1814';
      context.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale, 4 * scale);
      context.fillStyle = robot.team === 'red' ? '#ff5b4d' : '#54a7ff';
      context.fillRect(x - 20 * scale, y - 25 * scale, 40 * scale * (robot.hp / 100), 4 * scale);
      context.fillStyle = '#dfe9e4';
      context.font = `${Math.max(8, 9 * scale)}px DM Mono`;
      context.textAlign = 'center';
      context.fillText(robot.name, x, y + 30 * scale);
    }
  }

  onMount(() => {
    const observer = new ResizeObserver(([entry]) => { width = entry.contentRect.width; });
    observer.observe(canvas.parentElement!);
    return () => observer.disconnect();
  });

  $effect(() => { snapshot; width; draw(); });
</script>

<canvas bind:this={canvas} aria-label="Live robot arena"></canvas>
