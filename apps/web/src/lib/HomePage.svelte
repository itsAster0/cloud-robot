<script lang="ts">
  import { onMount } from 'svelte';
  import { loadTerrain, paintChunk, roadsFor } from './terrain';
  import { drawRobot, loadRobotSprites } from './robotSprites';
  import type { ArenaObstacle, WorldSite } from './types';

  let { signedIn, onSignIn }: { signedIn: boolean; onSignIn?: () => void } = $props();
  let canvas: HTMLCanvasElement | undefined = $state();

  // A small decorative scene built from the real map textures and tank
  // sprites. It is not a simulation; tanks follow fixed loops.
  const W = 1024, H = 576;
  const sites: WorldSite[] = [{ kind: 'armoury', x: 256, y: 288, biome: 'urban' }, { kind: 'bunker', x: 768, y: 288, biome: 'forest' }];
  const obstacles: ArenaObstacle[] = [
    { id: 'a', shape: 'aabb', x: 120, y: 120, width: 90, height: 24, material: 'brick' },
    { id: 'b', shape: 'aabb', x: 360, y: 400, width: 70, height: 70, material: 'crate' },
    { id: 'c', shape: 'aabb', x: 420, y: 110, width: 160, height: 60, material: 'container' },
    { id: 'd', shape: 'aabb', x: 640, y: 130, width: 70, height: 70, material: 'tree' },
    { id: 'e', shape: 'aabb', x: 880, y: 420, width: 80, height: 80, material: 'tree' },
    { id: 'f', shape: 'aabb', x: 700, y: 440, width: 70, height: 56, material: 'rock' },
    { id: 'g', shape: 'aabb', x: 150, y: 430, width: 44, height: 44, material: 'barrel' },
    { id: 'h', shape: 'aabb', x: 520, y: 470, width: 140, height: 26, material: 'sandbag' },
  ];
  const tanks = [
    { color: '#58dec5', cx: 256, cy: 288, rx: 150, ry: 90, speed: 0.35, phase: 0 },
    { color: '#ff6b5b', cx: 768, cy: 288, rx: 140, ry: 110, speed: -0.3, phase: 1.5 },
    { color: '#f0c274', cx: 512, cy: 300, rx: 260, ry: 60, speed: 0.22, phase: 3 },
    { color: '#54a7ff', cx: 620, cy: 230, rx: 90, ry: 140, speed: 0.4, phase: 4.2 },
  ];

  onMount(() => {
    if (!canvas) return;
    const reduce = (() => { try { return matchMedia('(prefers-reduced-motion: reduce)').matches; } catch { return false; } })();
    const ground = document.createElement('canvas');
    ground.width = W; ground.height = H;
    let painted = false;
    const paintGround = () => {
      const g = ground.getContext('2d')!;
      painted = paintChunk(g, 0, 0, W, sites, roadsFor(sites), [], obstacles);
    };
    loadTerrain(paintGround);
    loadRobotSprites(() => {});
    paintGround();
    const shots: { x: number; y: number; vx: number; vy: number; life: number }[] = [];
    let frame = 0, last = performance.now(), fired = 0;
    const c = canvas.getContext('2d')!;
    const draw = (now: number) => {
      const t = reduce ? 2 : now / 1000;
      const dt = Math.min(0.05, (now - last) / 1000); last = now;
      const dpr = Math.min(2, devicePixelRatio || 1);
      const cssW = canvas!.clientWidth, cssH = cssW * H / W;
      if (canvas!.width !== Math.round(cssW * dpr)) { canvas!.width = Math.round(cssW * dpr); canvas!.height = Math.round(cssH * dpr); }
      c.setTransform(canvas!.width / W, 0, 0, canvas!.height / H, 0, 0);
      if (painted) c.drawImage(ground, 0, 0); else { c.fillStyle = '#0d1a14'; c.fillRect(0, 0, W, H); }
      const positions = tanks.map(k => {
        const a = t * k.speed + k.phase;
        const x = k.cx + Math.cos(a) * k.rx, y = k.cy + Math.sin(a) * k.ry;
        const heading = Math.atan2(Math.cos(a) * k.ry * k.speed, -Math.sin(a) * k.rx * k.speed) * 180 / Math.PI;
        return { ...k, x, y, heading };
      });
      if (!reduce && now - fired > 450) {
        fired = now;
        const from = positions[Math.floor(Math.random() * positions.length)];
        const to = positions.filter(p => p !== from)[Math.floor(Math.random() * (positions.length - 1))];
        const d = Math.hypot(to.x - from.x, to.y - from.y) || 1;
        shots.push({ x: from.x, y: from.y, vx: (to.x - from.x) / d * 420, vy: (to.y - from.y) / d * 420, life: d / 420 });
      }
      c.fillStyle = '#eaf69f';
      for (let i = shots.length - 1; i >= 0; i--) {
        const s = shots[i];
        s.x += s.vx * dt; s.y += s.vy * dt; s.life -= dt;
        if (s.life <= 0) { shots.splice(i, 1); continue; }
        c.globalAlpha = 0.35; c.beginPath(); c.arc(s.x, s.y, 7, 0, Math.PI * 2); c.fill(); c.globalAlpha = 1;
        c.fillRect(s.x - 2.5, s.y - 2.5, 5, 5);
      }
      for (const p of positions) {
        const target = positions.filter(o => o !== p).reduce((best, o) => Math.hypot(o.x - p.x, o.y - p.y) < Math.hypot(best.x - p.x, best.y - p.y) ? o : best);
        c.globalAlpha = 0.35; c.fillStyle = p.color; c.beginPath(); c.arc(p.x, p.y, 38, 0, Math.PI * 2); c.fill(); c.globalAlpha = 1;
        // Drawn at 1.6x so the tanks read clearly in the small preview.
        c.save(); c.translate(p.x, p.y); c.scale(1.6, 1.6);
        const sprite = drawRobot(c, 0, 0, p.heading, Math.atan2(target.y - p.y, target.x - p.x) * 180 / Math.PI, p.color);
        c.restore();
        if (!sprite) {
          c.fillStyle = p.color; c.beginPath(); c.arc(p.x, p.y, 14, 0, Math.PI * 2); c.fill();
        }
      }
      if (!reduce) frame = requestAnimationFrame(draw);
    };
    frame = requestAnimationFrame(draw);
    // Reduced motion draws one still frame once the textures arrive.
    const still = reduce ? setTimeout(() => requestAnimationFrame(draw), 800) : 0;
    return () => { cancelAnimationFrame(frame); clearTimeout(still); };
  });

  const steps = [
    { n: '1', title: 'Pick a robot', text: 'Choose a personality: brawler, sniper, scout, and more. Each one is a small program that decides what to do ten times a second.' },
    { n: '2', title: 'Press play', text: 'The arena is set up for you. Your robot drops into a textured battlefield against server bots or other players.' },
    { n: '3', title: 'Watch and improve', text: 'Click any robot to see what it is doing. When you are curious, open its code and change how it thinks.' },
  ];
</script>

<section class="relative overflow-hidden">
  <div class="hero-glow pointer-events-none absolute inset-0" aria-hidden="true"></div>
  <div class="relative mx-auto grid max-w-6xl grid-cols-1 items-center gap-10 px-4 py-12 sm:px-8 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:py-20">
    <div class="grid gap-5">
      <span class="w-fit rounded-full border border-primary/40 bg-accent px-3 py-1 font-mono text-xs text-primary">Robot battles you program</span>
      <h1 class="m-0 text-4xl leading-[1.05] font-semibold tracking-tight sm:text-5xl">Build a robot.<br/><span class="text-primary">Watch it fight.</span></h1>
      <p class="m-0 max-w-lg text-lg text-muted-foreground">Pick a robot personality, press play, and see it battle in a live arena. No setup and no coding needed to start; the code is there when you want it.</p>
      <div class="flex flex-wrap items-center gap-3">
        {#if signedIn}
          <a class="cta inline-flex h-12 items-center rounded-xl bg-primary px-6 text-base font-semibold text-primary-foreground no-underline" href="#/play">Play your first match ▶</a>
        {:else}
          <button type="button" class="cta m-0 h-12 cursor-pointer rounded-xl border-0 bg-primary px-6 text-base font-semibold text-primary-foreground" onclick={onSignIn}>Sign in and play ▶</button>
        {/if}
        <a class="inline-flex h-12 items-center rounded-xl border border-border px-5 text-sm text-foreground no-underline hover:border-primary/60" href="#/matches">Watch matches</a>
      </div>
    </div>
    <div class="arena-frame rounded-2xl border border-border bg-card p-2">
      <canvas bind:this={canvas} class="block aspect-[16/9] w-full rounded-xl" aria-label="Animated preview of tanks battling in a textured arena"></canvas>
    </div>
  </div>
</section>

<section class="mx-auto grid max-w-6xl grid-cols-1 gap-4 px-4 pb-16 sm:px-8 md:grid-cols-3">
  {#each steps as step, i}
    <article class="step rounded-2xl border border-border bg-card p-6" style:animation-delay={`${i * 120}ms`}>
      <span class="grid size-9 place-items-center rounded-full bg-primary font-semibold text-primary-foreground">{step.n}</span>
      <h2 class="mt-4 mb-2 text-lg font-semibold">{step.title}</h2>
      <p class="m-0 text-sm leading-relaxed text-muted-foreground">{step.text}</p>
    </article>
  {/each}
</section>

<style>
  .hero-glow { background: radial-gradient(60% 60% at 75% 30%, color-mix(in srgb, var(--color-primary) 18%, transparent), transparent 70%), radial-gradient(40% 50% at 10% 80%, color-mix(in srgb, var(--color-team-blue) 12%, transparent), transparent 70%); }
  .arena-frame { box-shadow: 0 30px 80px -30px color-mix(in srgb, var(--color-primary) 45%, transparent); }
  .cta { transition: transform .15s ease, filter .15s ease; box-shadow: 0 10px 40px -12px var(--color-primary); }
  .cta:hover { transform: translateY(-1px) scale(1.02); filter: brightness(1.08); }
  .step { animation: rise .6s ease both; }
  @keyframes rise { from { opacity: 0; transform: translateY(12px); } }
  @media (prefers-reduced-motion: reduce) { .step { animation: none; } .cta:hover { transform: none; } }
</style>
