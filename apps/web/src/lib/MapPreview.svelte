<script lang="ts">
  import { siteColor, siteLabel } from './matchStats';
  export interface MapPreviewData {
    width: number;
    height: number;
    sites: { x: number; y: number; kind: string }[];
    obstacles: { x: number; y: number; width: number; height: number; material?: string }[];
    transit: { x: number; y: number }[];
    containers: { x: number; y: number }[];
    hazards: { x: number; y: number; width: number; height: number; kind: string }[];
  }

  let { preview }: { preview: MapPreviewData | null } = $props();
  let canvas: HTMLCanvasElement | undefined = $state();

  $effect(() => {
    const c = canvas?.getContext('2d');
    if (!c || !canvas) return;
    const W = (canvas.width = 640);
    const H = (canvas.height = 400);
    c.fillStyle = '#07130d';
    c.fillRect(0, 0, W, H);
    if (!preview) {
      c.fillStyle = '#5d7268';
      c.font = '12px monospace';
      c.fillText('No preview — sign in and tune the arena.', 24, H / 2);
      return;
    }
    const scale = Math.min(W / preview.width, H / preview.height);
    const ox = (W - preview.width * scale) / 2;
    const oy = (H - preview.height * scale) / 2;
    const X = (x: number) => ox + x * scale;
    const Y = (y: number) => oy + y * scale;
    // Ground speckle so the preview reads as terrain.
    for (let n = 0; n < 220; n++) {
      const hx = (n * 374761393 + 7) % 997, hy = (n * 668265263 + 13) % 991;
      c.fillStyle = n % 2 ? '#0a1f16' : '#0d241b';
      c.fillRect(ox + (hx / 997) * preview.width * scale, oy + (hy / 991) * preview.height * scale, 2, 2);
    }
    c.strokeStyle = '#1d3329';
    c.lineWidth = 1;
    for (let gx = 0; gx <= preview.width; gx += 1024) {
      c.beginPath(); c.moveTo(X(gx), oy); c.lineTo(X(gx), oy + preview.height * scale); c.stroke();
    }
    for (const h of preview.hazards ?? []) {
      c.fillStyle = h.kind === 'slow' ? 'rgba(255,200,87,0.16)' : 'rgba(255,91,77,0.18)';
      c.fillRect(X(h.x), Y(h.y), Math.max(2, h.width * scale), Math.max(2, h.height * scale));
    }
    const skins: Record<string, [string, string]> = {
      hedge: ['#2e5b34', '#6fae6f'],
      glass: ['rgba(127,179,200,0.35)', '#9fd4e8'],
      rock: ['#57503f', '#8a7f63'],
      wall: ['#304e42', '#668b75'],
    };
    for (const o of preview.obstacles ?? []) {
      const [fill, stroke] = skins[o.material ?? 'wall'] ?? skins.wall;
      c.fillStyle = fill; c.strokeStyle = stroke; c.lineWidth = 1;
      const w = Math.max(1.5, o.width * scale), h = Math.max(1.5, o.height * scale);
      c.fillRect(X(o.x), Y(o.y), w, h);
      c.strokeRect(X(o.x), Y(o.y), w, h);
    }
    c.fillStyle = '#efc86b';
    for (const item of preview.containers ?? []) c.fillRect(X(item.x) - 1, Y(item.y) - 1, 2, 2);
    c.strokeStyle = '#b58dff';
    c.lineWidth = 1.5;
    for (const t of preview.transit ?? []) {
      c.beginPath(); c.arc(X(t.x), Y(t.y), 4, 0, Math.PI * 2); c.stroke();
    }
    for (const s of preview.sites ?? []) {
      const color = siteColor[s.kind] ?? '#9cb8a5';
      c.globalAlpha = 0.16; c.fillStyle = color;
      c.beginPath(); c.arc(X(s.x), Y(s.y), 300 * scale, 0, Math.PI * 2); c.fill();
      c.globalAlpha = 1;
      c.fillStyle = color;
      c.beginPath(); c.arc(X(s.x), Y(s.y), 3, 0, Math.PI * 2); c.fill();
      c.font = '9px monospace'; c.textAlign = 'center';
      c.fillText(siteLabel(s.kind), X(s.x), Y(s.y) - 310 * scale);
      c.textAlign = 'left';
    }
    c.strokeStyle = '#648272';
    c.strokeRect(ox, oy, preview.width * scale, preview.height * scale);
  });
</script>

<div class="map-preview">
  <canvas bind:this={canvas} aria-label="Generated arena preview"></canvas>
  {#if preview}
    <p>{preview.sites.length} sites · {preview.obstacles.length} cover · {preview.containers.length} loot · {preview.hazards.length} hazards · {Math.round(preview.width)} × {Math.round(preview.height)}</p>
  {/if}
</div>

<style>
  .map-preview canvas { width: 100%; display: block; border: 1px solid #304d3b; background: #07130d; }
  .map-preview p { font: 10px 'DM Mono'; color: #a3b9a9; margin: 8px 0 0; }
</style>
