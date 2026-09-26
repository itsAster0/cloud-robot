<script lang="ts">
  import { onMount } from 'svelte';
  import { SnapshotBuffer, angleBetween } from './interpolation';
  import { hpFraction, siteColor, teamColor, weaponGlyph } from './matchStats';
  import type { Snapshot, RobotState } from './types';
  export interface SiteMark { x: number; y: number; kind: string }
  let { snapshot, selected = '', leaderId = '', overview = [], sites = [], onregion }: { snapshot: Snapshot | null; selected?: string; leaderId?: string; overview?: RobotState[]; sites?: SiteMark[]; onregion?: (region: {x:number;y:number;width:number;height:number}) => void } = $props();
  function hash2(x: number, y: number, seed: number) {
    let h = (Math.imul(x, 374761393) + Math.imul(y, 668265263) + Math.imul(seed, 974634211)) | 0;
    h = Math.imul(h ^ (h >>> 13), 1274126177);
    return ((h ^ (h >>> 16)) >>> 0) / 4294967295;
  }
  let regionAt = 0;
  let canvas: HTMLCanvasElement;
  let zoom = $state(1), follow = $state(true), fps = $state(0), frameP95 = $state(0), age = $state(0);
  let camera = { x: 0, y: 0 }, target = { x: 0, y: 0 }, dragged = false, last = { x: 0, y: 0 };
  let keys = new Set<string>();
  let received = 0, width = 900, height = 580;
  const buffer = new SnapshotBuffer();
  const tiles = new Map<string, HTMLCanvasElement>();
  let layoutKey = '';
  const chunks = new Map<string, NonNullable<Snapshot['obstacles']>>();
  $effect(() => {
    if (!snapshot) return;
    received = performance.now(); buffer.push(snapshot, received);
    const key = `${snapshot.matchId}:${snapshot.revision ?? 1}`;
    if (key !== layoutKey) {
      layoutKey = key; tiles.clear(); chunks.clear();
      for (const obstacle of snapshot.obstacles ?? []) {
        for (let x = Math.floor(obstacle.x / 1024); x <= Math.floor((obstacle.x + (obstacle.width ?? 0)) / 1024); x++) {
          for (let y = Math.floor(obstacle.y / 1024); y <= Math.floor((obstacle.y + (obstacle.height ?? 0)) / 1024); y++) {
            const k = `${x}:${y}`; const list = chunks.get(k) ?? []; list.push(obstacle); chunks.set(k, list);
          }
        }
      }
    }
  });
  function tile(x: number, y: number) {
    const key = `${x}:${y}`; let t = tiles.get(key);
    if (t) return t;
    if (tiles.size >= 48) tiles.delete(tiles.keys().next().value!);
    t = document.createElement('canvas'); t.width = 512; t.height = 512;
    const c = t.getContext('2d')!; c.scale(.5, .5); c.fillStyle = '#071712'; c.fillRect(0, 0, 1024, 1024);
    // Deterministic ground texture: two speckle layers plus grass tufts keyed
    // by tile so the map reads as terrain, not flat fill. Cached per tile.
    for (let n = 0; n < 46; n++) {
      const px = hash2(x, n, 7) * 1024, py = hash2(n, y, 13) * 1024, s = 2 + hash2(x + n, y - n, 29) * 5;
      c.fillStyle = n % 2 ? '#0a1f16' : '#0d241b';
      c.fillRect(px, py, s, s);
    }
    c.strokeStyle = '#1e4630'; c.lineWidth = 2;
    for (let n = 0; n < 7; n++) {
      const px = hash2(x, n, 41) * 1024, py = hash2(n, y, 43) * 1024;
      c.beginPath(); c.moveTo(px, py); c.lineTo(px + 3, py - 9); c.moveTo(px + 5, py); c.lineTo(px + 8, py - 7); c.stroke();
    }
    c.strokeStyle = '#183329'; c.lineWidth = 1;
    for (let n = 0; n <= 1024; n += 64) { c.beginPath(); c.moveTo(n, 0); c.lineTo(n, 1024); c.moveTo(0, n); c.lineTo(1024, n); c.stroke(); }
    const skins: Record<string, [string, string]> = {
      hedge: ['#2e5b34', '#6fae6f'],
      glass: ['rgba(127,179,200,0.30)', '#9fd4e8'],
      rock: ['#57503f', '#8a7f63'],
      wall: ['#304e42', '#668b75'],
    };
    for (const o of chunks.get(key) ?? []) {
      const [fill, stroke] = skins[o.material ?? 'wall'] ?? skins.wall;
      c.fillStyle = fill; c.strokeStyle = stroke; c.lineWidth = 3;
      c.fillRect(o.x - x * 1024, o.y - y * 1024, o.width ?? 0, o.height ?? 0);
      c.strokeRect(o.x - x * 1024, o.y - y * 1024, o.width ?? 0, o.height ?? 0);
    }
    tiles.set(key, t); return t;
  }
  onMount(() => {
    const down = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT')) return;
      const k = e.key.toLowerCase();
      if (['arrowup','arrowdown','arrowleft','arrowright','w','a','s','d'].includes(k)) { keys.add(k); e.preventDefault(); }
    };
    const up = (e: KeyboardEvent) => keys.delete(e.key.toLowerCase());
    window.addEventListener('keydown', down);
    window.addEventListener('keyup', up);
    let frame = 0, frames = 0, measured = performance.now(), active = true;
    let lastFrame=0; const intervals:number[]=[];
    const resize = new ResizeObserver(([e]) => { width = Math.max(300, e.contentRect.width); height = Math.min(680, Math.max(420, width * .6)); });
    resize.observe(canvas.parentElement!);
    const draw = (now: number) => {
      if (!active) return;
      if(lastFrame && now-lastFrame<1000 && !document.hidden) { intervals.push(now-lastFrame); if(intervals.length>300)intervals.shift(); } else intervals.length=0; lastFrame=now;
      const dpr = Math.min(2, devicePixelRatio || 1);
      if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(height * dpr)) { canvas.width = Math.round(width * dpr); canvas.height = Math.round(height * dpr); canvas.style.height = `${height}px`; }
      const c = canvas.getContext('2d')!; c.setTransform(dpr, 0, 0, dpr, 0, 0); c.fillStyle = '#030b08'; c.fillRect(0, 0, width, height);
      const sample = buffer.sample(now), s = sample?.after ?? snapshot;
      if (s) {
        const old = new Map(sample?.before.robots.map(r => [r.robotId, r]) ?? []);
        const positions = s.robots.map(r => { const p = old.get(r.robotId) ?? r; const t = s.events?.some(e => e.type === 'teleport' && e.robotId === r.robotId) ? 1 : sample?.amount ?? 1; return { ...r, x: p.x + (r.x - p.x) * t, y: p.y + (r.y - p.y) * t, heading: angleBetween(p.heading, r.heading, t), turretHeading: angleBetween(p.turretHeading ?? p.heading, r.turretHeading ?? r.heading, t) }; });
        const focus = (positions.find(r => r.robotId === selected) ?? overview.find(r => r.robotId === selected)) ?? positions.find(r => r.alive) ?? overview.find(r => r.alive) ?? positions[0] ?? overview[0];
        if (focus && follow) target = { x: focus.x, y: focus.y };
        if (focus && camera.x === 0 && camera.y === 0 && target.x === 0 && target.y === 0) {
          camera = { x: focus.x, y: focus.y };
          target = { x: focus.x, y: focus.y };
        }
        // Smooth stroll: ease the camera toward its target so large maps
        // feel continuous instead of snapping every snapshot.
        const scale = zoom;
        const pan = 900 / scale;
        if (keys.size) {
          follow = false;
          if (keys.has('arrowleft') || keys.has('a')) target.x -= pan * 0.05;
          if (keys.has('arrowright') || keys.has('d')) target.x += pan * 0.05;
          if (keys.has('arrowup') || keys.has('w')) target.y -= pan * 0.05;
          if (keys.has('arrowdown') || keys.has('s')) target.y += pan * 0.05;
        }
        camera.x += (target.x - camera.x) * 0.14;
        camera.y += (target.y - camera.y) * 0.14;
        if (Math.abs(target.x - camera.x) < 0.5) camera.x = target.x;
        if (Math.abs(target.y - camera.y) < 0.5) camera.y = target.y;
        const visible = (x: number, y: number, pad = 50) => Math.abs(x - camera.x) < width / scale / 2 + pad && Math.abs(y - camera.y) < height / scale / 2 + pad;
        c.save(); c.translate(width / 2, height / 2); c.scale(scale, scale); c.translate(-camera.x, -camera.y);
        const left = Math.max(0, Math.floor((camera.x - width / scale / 2) / 1024)), right = Math.min(Math.ceil((s.width ?? 42000) / 1024) - 1, Math.floor((camera.x + width / scale / 2) / 1024));
        const top = Math.max(0, Math.floor((camera.y - height / scale / 2) / 1024)), bottom = Math.min(Math.ceil((s.height ?? 26250) / 1024) - 1, Math.floor((camera.y + height / scale / 2) / 1024));
        for (let x = left; x <= right; x++) for (let y = top; y <= bottom; y++) c.drawImage(tile(x, y), x * 1024, y * 1024, 1024, 1024);
        for (const site of sites) {
          if (!visible(site.x, site.y, 300)) continue;
          const color = siteColor[site.kind] ?? '#9cb8a5';
          c.globalAlpha = 0.12; c.fillStyle = color;
          c.beginPath(); c.arc(site.x, site.y, 300, 0, Math.PI * 2); c.fill();
          c.globalAlpha = 0.5; c.strokeStyle = color; c.lineWidth = 2 / scale;
          c.beginPath(); c.arc(site.x, site.y, 300, 0, Math.PI * 2); c.stroke();
          c.globalAlpha = 1;
          if (scale >= 0.45) {
            c.fillStyle = color; c.font = `${13 / scale}px monospace`; c.textAlign = 'center';
            c.fillText(site.kind.replace(/_/g, ' ').toUpperCase(), site.x, site.y - 310);
            c.textAlign = 'left';
          }
        }
        for (const item of s.items ?? []) if (item.active && visible(item.x, item.y)) { c.fillStyle = '#efc86b'; c.fillRect(item.x - 8, item.y - 8, 16, 16); }
        for (const link of s.transit ?? []) if (visible(link.x, link.y)) { c.strokeStyle = '#b58dff'; c.lineWidth = 3; c.beginPath(); c.arc(link.x, link.y, 24, 0, Math.PI * 2); c.stroke(); }
        if(now-regionAt>=100) { regionAt=now; onregion?.({x:Math.max(0,Math.min(48000,camera.x)),y:Math.max(0,Math.min(30000,camera.y)),width:Math.max(100,Math.min(48000,width/scale)),height:Math.max(100,Math.min(30000,height/scale))}); }
        const previous = new Map(sample?.before.projectiles.map(p => [p.projectileId, p]) ?? []);
        for (const p of s.projectiles) { const old = previous.get(p.projectileId) ?? p, t = sample?.amount ?? 1, x = old.x + (p.x - old.x) * t, y = old.y + (p.y - old.y) * t; if (!visible(x, y)) continue; c.fillStyle = '#eaf69f'; c.fillRect(x - 2, y - 2, 4, 4); }
        for (const r of positions) {
          if (!visible(r.x, r.y)) continue;
          const color = teamColor(r.team);
          const isFocus = r.robotId === focus?.robotId;
          const isLeader = r.robotId === leaderId;
          c.save(); c.translate(r.x, r.y); c.rotate(r.heading * Math.PI / 180);
          c.fillStyle = !r.alive ? '#38483e' : isFocus ? '#ffffff' : color;
          c.beginPath(); c.moveTo(18, 0); c.lineTo(-12, -13); c.lineTo(-8, 0); c.lineTo(-12, 13); c.closePath(); c.fill(); c.restore();
          if (!r.alive) {
            c.strokeStyle = '#5a6a61'; c.lineWidth = 2;
            c.beginPath(); c.moveTo(r.x - 8, r.y - 8); c.lineTo(r.x + 8, r.y + 8); c.moveTo(r.x + 8, r.y - 8); c.lineTo(r.x - 8, r.y + 8); c.stroke();
            continue;
          }
          c.strokeStyle = '#f4f5df'; c.lineWidth = 4; c.beginPath(); c.moveTo(r.x, r.y); const a = (r.turretHeading ?? r.heading) * Math.PI / 180; c.lineTo(r.x + Math.cos(a) * 24, r.y + Math.sin(a) * 24); c.stroke();
          // Shield over hull: every living robot reads at a glance.
          const shieldMax = r.maxShield ?? 50, shieldFrac = Math.max(0, Math.min(1, (r.shield ?? 0) / shieldMax));
          if (shieldFrac > 0) { c.fillStyle = '#1d3a44'; c.fillRect(r.x - 20, r.y - 33, 40, 3); c.fillStyle = '#54d8ff'; c.fillRect(r.x - 20, r.y - 33, 40 * shieldFrac, 3); }
          const hp = hpFraction(r);
          c.fillStyle = '#273b30'; c.fillRect(r.x - 20, r.y - 27, 40, 4);
          c.fillStyle = hp > 0.5 ? '#adf08a' : hp > 0.25 ? '#ffc857' : '#ff5b4d';
          c.fillRect(r.x - 20, r.y - 27, 40 * hp, 4);
          // Power icon: active weapon glyph above the hull.
          c.fillStyle = '#e5eadf'; c.font = '11px monospace'; c.textAlign = 'center';
          c.fillText(weaponGlyph(r.weapon), r.x, r.y - 38);
          if (isLeader) { c.fillStyle = '#dfff86'; c.font = '12px monospace'; c.fillText('♛', r.x, r.y - 50); }
          if (isFocus) {
            c.strokeStyle = '#ffffff60'; c.lineWidth = 1.5; c.beginPath(); c.arc(r.x, r.y, 24, 0, Math.PI * 2); c.stroke();
            c.strokeStyle = '#dfff8640'; c.lineWidth = 1; c.beginPath(); c.arc(r.x, r.y, r.visionRange ?? 600, 0, Math.PI * 2); c.stroke();
          }
          if (isFocus || isLeader || scale >= 0.9) {
            c.fillStyle = isFocus ? '#ffffff' : '#c9d6cb'; c.font = '12px monospace';
            c.fillText(isLeader && !isFocus ? `♛ ${r.name}` : r.name, r.x, r.y + 38);
          }
          c.textAlign = 'left';
        }
        if (s.zone) { c.strokeStyle = '#ed815a'; c.lineWidth = 3 / scale; c.beginPath(); c.arc(s.zone.x, s.zone.y, s.zone.radius, 0, Math.PI * 2); c.stroke(); }
        c.restore();
        const mw = 180, mh = mw * (s.height ?? 26250) / (s.width ?? 42000), mx = width - mw - 12, my = 12, mapScale = mw / (s.width ?? 42000);
        c.fillStyle = '#07110fee'; c.fillRect(mx, my, mw, mh); c.strokeStyle = '#648272'; c.strokeRect(mx, my, mw, mh);
        for (const r of (overview.length ? overview : positions)) if (r.alive) { c.fillStyle = r.robotId === leaderId ? '#dfff86' : teamColor(r.team); c.fillRect(mx + r.x * mapScale - 1, my + r.y * mapScale - 1, 2, 2); }
        c.strokeStyle = '#eee'; c.strokeRect(mx + (camera.x - width / scale / 2) * mapScale, my + (camera.y - height / scale / 2) * mapScale, width / scale * mapScale, height / scale * mapScale);
      }
      frames++; if (now - measured >= 500) { fps = Math.round(frames * 1000 / (now - measured)); const sorted=[...intervals].sort((a,b)=>a-b); frameP95=sorted.length ? sorted[Math.ceil(sorted.length*.95)-1] : 0; age = Math.round(now - received); frames = 0; measured = now; }
      frame = requestAnimationFrame(draw);
    };
    frame = requestAnimationFrame(draw);
    return () => { active = false; cancelAnimationFrame(frame); resize.disconnect(); tiles.clear(); window.removeEventListener('keydown', down); window.removeEventListener('keyup', up); };
  });
  function wheel(e: WheelEvent) {
    e.preventDefault();
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left - width / 2, my = e.clientY - rect.top - height / 2;
    const next = Math.max(.1, Math.min(4, zoom * Math.exp(-e.deltaY * .001)));
    // Zoom toward the cursor so large maps stay navigable.
    target.x += mx / zoom - mx / next;
    target.y += my / zoom - my / next;
    camera.x += mx / zoom - mx / next;
    camera.y += my / zoom - my / next;
    zoom = next;
  }
  function jumpMinimap(e: MouseEvent) {
    if (!snapshot) return;
    const rect = canvas.getBoundingClientRect();
    const w = snapshot.width ?? 42000, h = snapshot.height ?? 26250;
    const mw = 180, mh = mw * h / w, mx = width - mw - 12, my = 12;
    const px = e.clientX - rect.left, py = e.clientY - rect.top;
    const scaleY = rect.height / height;
    if (px / scaleY < mx || py / scaleY > my + mh) return;
    target = { x: ((px / scaleY - mx) / mw) * w, y: ((py / scaleY - my) / mh) * h };
    follow = false;
  }
</script>
<div class="world">
  <div class="toolbar"><button onclick={() => follow = !follow}>{follow ? 'Following robot' : 'Free camera'}</button><button onclick={() => zoom = Math.min(4, zoom * 1.3)}>Zoom +</button><button onclick={() => zoom = Math.max(.1, zoom / 1.3)}>Zoom −</button><button onclick={() => { if (snapshot) { target = { x: (snapshot.width ?? 42000) / 2, y: (snapshot.height ?? 26250) / 2 }; follow = false; } }}>Center map</button><span>{snapshot ? `${fps} fps · frame p95 ${frameP95.toFixed(1)} ms · update ${age} ms ago` : "Waiting for match state"}</span></div>
  <canvas bind:this={canvas} aria-label="Robot arena. Drag to pan, scroll to zoom, use WASD or arrow keys to stroll. Click the minimap to jump." onwheel={wheel} onclick={jumpMinimap} onpointerdown={(e) => { dragged = true; follow = false; last = { x: e.clientX, y: e.clientY }; canvas.setPointerCapture(e.pointerId); }} onpointermove={(e) => { if (dragged) { target.x -= (e.clientX - last.x) / zoom; target.y -= (e.clientY - last.y) / zoom; camera.x -= (e.clientX - last.x) / zoom; camera.y -= (e.clientY - last.y) / zoom; last = { x: e.clientX, y: e.clientY }; } }} onpointerup={() => dragged = false} onpointercancel={() => dragged = false}></canvas>
  <div class="legend" aria-label="Map legend">♛ kill leader · ◉ plasma ⋮ machine-gun ∴ shotgun ● cannon ┃ railgun ✸ grenade ♨ incendiary ❄ cryo ⚡ emp · cyan bar shield · team colors on hulls and minimap</div>
</div>
<style>
  .world{border:1px solid #345143;background:#07110e;min-width:0}.toolbar{display:flex;gap:8px;padding:10px;align-items:center;flex-wrap:wrap}.toolbar button{padding:8px;background:#192b22;color:#d8e9dc;border:1px solid #42614e}.toolbar span{margin-left:auto;color:#9cb8a5;font-size:12px}canvas{width:100%;display:block;touch-action:none}.legend{padding:8px 12px;color:#7d917f;font:10px monospace;border-top:1px solid #243e2e}
</style>
