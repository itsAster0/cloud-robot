import atlasUrl from './assets/terrain-atlas.png';
import type { ArenaObstacle, WorldHazard, WorldSite } from './types';

// Cell order of assets/terrain-atlas.png (8 columns of 64 px cells).
// Rebuild the image with scripts/build-terrain-atlas.sh.
export const ATLAS = [
  'grass_a', 'grass_b', 'grass_c', 'dirt_a', 'dirt_b', 'stone_a', 'stone_b', 'stone_c',
  'sand_a', 'sand_b', 'asphalt', 'water', 'plaza', 'brick', 'plank', 'tanbrick',
  'crate', 'crate_small', 'rock', 'rock_b', 'barrel', 'barrel_grey', 'bush', 'tree',
  'tree_autumn',
] as const;
export type TileName = (typeof ATLAS)[number];

// First tile is each biome's base ground; the rest are occasional variants.
const GROUND: Record<string, TileName[]> = {
  urban: ['stone_b', 'stone_a', 'stone_c'],
  industrial: ['asphalt', 'stone_c', 'dirt_b'],
  forest: ['grass_a', 'grass_b', 'dirt_a'],
  desert: ['sand_a', 'sand_b', 'dirt_b'],
  snow: ['stone_a', 'stone_b', 'sand_a'],
  swamp: ['grass_c', 'dirt_a', 'grass_b'],
};
// Biomes without their own atlas tiles are washed over a base tile.
const GROUND_TINT: Record<string, string> = {
  snow: 'rgba(236,244,252,.78)',
  swamp: 'rgba(52,62,26,.5)',
};
const VARIANT_RATE = 0.12;
// World units per ground texture cell.
export const GROUND_CELL = 128;
export const ROAD_HALF_WIDTH = 90;

export function hash2(x: number, y: number, seed: number) {
  let h = (Math.imul(x, 374761393) + Math.imul(y, 668265263) + Math.imul(seed, 974634211)) | 0;
  h = Math.imul(h ^ (h >>> 13), 1274126177);
  return ((h ^ (h >>> 16)) >>> 0) / 4294967295;
}

/** Nearest site's biome, with a hashed offset so district borders look organic. */
export function biomeAt(sites: WorldSite[], x: number, y: number): string {
  if (!sites.length) return 'forest';
  const cx = Math.floor(x / GROUND_CELL), cy = Math.floor(y / GROUND_CELL);
  const px = x + (hash2(cx, cy, 3) - 0.5) * 420, py = y + (hash2(cx, cy, 5) - 0.5) * 420;
  let best = sites[0], bestDistance = Infinity;
  for (const site of sites) {
    const d = (site.x - px) ** 2 + (site.y - py) ** 2;
    if (d < bestDistance) { bestDistance = d; best = site; }
  }
  return best.biome && GROUND[best.biome] ? best.biome : 'forest';
}

export interface Road { x: number; y: number; width: number; height: number }

/** Axis-aligned roads between grid-neighbour sites, which share a row or column. */
export function roadsFor(sites: WorldSite[]): Road[] {
  const roads: Road[] = [];
  for (const a of sites) {
    let east: WorldSite | undefined, south: WorldSite | undefined;
    for (const b of sites) {
      if (Math.abs(b.y - a.y) < 1 && b.x > a.x && (!east || b.x < east.x)) east = b;
      if (Math.abs(b.x - a.x) < 1 && b.y > a.y && (!south || b.y < south.y)) south = b;
    }
    if (east) roads.push({ x: a.x, y: a.y - ROAD_HALF_WIDTH, width: east.x - a.x, height: ROAD_HALF_WIDTH * 2 });
    if (south) roads.push({ x: a.x - ROAD_HALF_WIDTH, y: a.y, width: ROAD_HALF_WIDTH * 2, height: south.y - a.y });
  }
  return roads;
}

let atlas: HTMLImageElement | null = null;
let atlasReady = false;
const sprites = new Map<TileName, HTMLCanvasElement>();

/** Loads the atlas once; `onReady` fires after it decodes so callers can repaint. */
export function loadTerrain(onReady: () => void) {
  if (atlasReady) return;
  if (!atlas) { atlas = new Image(); atlas.src = atlasUrl; }
  atlas.addEventListener('load', () => { atlasReady = true; onReady(); }, { once: true });
}

function sprite(name: TileName) {
  let s = sprites.get(name);
  if (s || !atlas) return s;
  const index = ATLAS.indexOf(name);
  s = document.createElement('canvas'); s.width = 64; s.height = 64;
  s.getContext('2d')!.drawImage(atlas, (index % 8) * 64, Math.floor(index / 8) * 64, 64, 64, 0, 0, 64, 64);
  sprites.set(name, s);
  return s;
}

const containerColors = ['#b85a1e', '#2f7a46', '#2b5f8a', '#8a2f2a', '#7a6a2a'];

/**
 * Paints one static 1024-unit chunk: ground, roads, site plazas, hazards, and
 * obstacles. `c` must already be translated so world (ox, oy) is its origin.
 * Returns false before the atlas has loaded; callers then draw flat fallback.
 */
export function paintChunk(
  c: CanvasRenderingContext2D, ox: number, oy: number, size: number,
  sites: WorldSite[], roads: Road[], hazards: WorldHazard[], obstacles: ArenaObstacle[],
) {
  if (!atlasReady) return false;
  const pattern = (name: TileName, scale = 1) => {
    const p = c.createPattern(sprite(name)!, 'repeat')!;
    // Patterns anchor at world origin so textures stay seamless across chunks.
    p.setTransform(new DOMMatrix().translateSelf(-ox, -oy).scaleSelf(scale * GROUND_CELL / 64));
    return p;
  };
  const snowCells: [number, number][] = [];
  for (let gx = 0; gx < size; gx += GROUND_CELL) {
    for (let gy = 0; gy < size; gy += GROUND_CELL) {
      const wx = ox + gx, wy = oy + gy;
      const biome = biomeAt(sites, wx + GROUND_CELL / 2, wy + GROUND_CELL / 2);
      const options = GROUND[biome];
      const roll = hash2(wx / GROUND_CELL, wy / GROUND_CELL, 11);
      const name = roll < VARIANT_RATE ? options[1 + Math.floor((roll / VARIANT_RATE) * (options.length - 1))] : options[0];
      c.drawImage(sprite(name)!, gx, gy, GROUND_CELL + 0.5, GROUND_CELL + 0.5);
      const tint = GROUND_TINT[biome];
      if (tint) { c.fillStyle = tint; c.fillRect(gx, gy, GROUND_CELL + 0.5, GROUND_CELL + 0.5); }
      if (biome === 'snow') snowCells.push([gx, gy]);
    }
  }
  const inChunk = (x: number, y: number, w: number, h: number, pad = 0) =>
    x - pad < ox + size && x + w + pad > ox && y - pad < oy + size && y + h + pad > oy;
  for (const road of roads) {
    if (!inChunk(road.x, road.y, road.width, road.height, ROAD_HALF_WIDTH)) continue;
    const horizontal = road.width > road.height;
    const x = road.x - ox, y = road.y - oy;
    c.fillStyle = pattern('asphalt');
    c.fillRect(x, y, road.width, road.height);
    c.fillStyle = '#c9c3a8';
    if (horizontal) {
      c.fillRect(x, y + 6, road.width, 4); c.fillRect(x, y + road.height - 10, road.width, 4);
      c.fillStyle = '#e8c547';
      for (let d = 40; d < road.width - 40; d += 120) c.fillRect(x + d, y + road.height / 2 - 3, 60, 6);
    } else {
      c.fillRect(x + 6, y, 4, road.height); c.fillRect(x + road.width - 10, y, 4, road.height);
      c.fillStyle = '#e8c547';
      for (let d = 40; d < road.height - 40; d += 120) c.fillRect(x + road.width / 2 - 3, y + d, 6, 60);
    }
  }
  for (const site of sites) {
    if (!inChunk(site.x - 280, site.y - 280, 560, 560)) continue;
    c.fillStyle = pattern(site.biome === 'forest' || site.biome === 'swamp' ? 'dirt_a' : site.biome === 'desert' ? 'dirt_b' : 'plaza');
    c.beginPath(); c.arc(site.x - ox, site.y - oy, 260, 0, Math.PI * 2); c.fill();
    if (site.biome === 'snow') { c.fillStyle = 'rgba(230,240,250,.45)'; c.fill(); }
    c.strokeStyle = 'rgba(20,30,26,.55)'; c.lineWidth = 10; c.stroke();
  }
  // Tone the bright tile palette down so robots, shots, and HUD stay legible.
  c.fillStyle = 'rgba(6,14,12,.38)'; c.fillRect(0, 0, size, size);
  // Snowfields stay bright after the tone-down so the biome reads as snow.
  c.fillStyle = 'rgba(214,228,240,.42)';
  for (const [gx, gy] of snowCells) c.fillRect(gx, gy, GROUND_CELL + 0.5, GROUND_CELL + 0.5);
  for (const h of hazards) {
    if (!inChunk(h.x, h.y, h.width, h.height)) continue;
    const x = h.x - ox, y = h.y - oy;
    const ground = biomeAt(sites, h.x + h.width / 2, h.y + h.height / 2);
    if (h.kind === 'slow' && ground === 'snow') {
      // Snowdrift: soft white mound with wind streaks.
      const drift = c.createRadialGradient(x + h.width / 2, y + h.height / 2, 8, x + h.width / 2, y + h.height / 2, Math.max(h.width, h.height) / 1.7);
      drift.addColorStop(0, '#ffffff'); drift.addColorStop(0.7, '#dfe9f3'); drift.addColorStop(1, 'rgba(210,225,240,.2)');
      c.fillStyle = drift; c.beginPath(); c.roundRect(x, y, h.width, h.height, Math.min(h.width, h.height) / 2); c.fill();
      c.strokeStyle = 'rgba(150,175,200,.6)'; c.lineWidth = 3;
      for (let n = 1; n < 4; n++) { c.beginPath(); c.moveTo(x + h.width * 0.15, y + h.height * n / 4); c.quadraticCurveTo(x + h.width / 2, y + h.height * n / 4 - 14, x + h.width * 0.85, y + h.height * n / 4); c.stroke(); }
    } else if (h.kind === 'slow' && ground === 'swamp') {
      // Bog: murky water with lily pads.
      c.fillStyle = '#34401c'; c.beginPath(); c.roundRect(x, y, h.width, h.height, 40); c.fill();
      c.globalAlpha = 0.45; c.fillStyle = pattern('water'); c.fill(); c.globalAlpha = 1;
      c.fillStyle = '#5f8a2e';
      for (let n = 0; n < 7; n++) { c.beginPath(); c.arc(x + hash2(n, h.x, 51) * h.width, y + hash2(h.y, n, 53) * h.height, 9 + n % 3 * 3, 0.4, Math.PI * 2); c.fill(); }
      c.strokeStyle = '#6b7a35'; c.lineWidth = 4; c.setLineDash([14, 10]); c.strokeRect(x, y, h.width, h.height); c.setLineDash([]);
    } else if (h.kind === 'slow') {
      c.globalAlpha = 0.75; c.fillStyle = pattern('water'); c.fillRect(x, y, h.width, h.height); c.globalAlpha = 1;
      c.strokeStyle = '#7fc4ec'; c.lineWidth = 4; c.setLineDash([18, 12]); c.strokeRect(x, y, h.width, h.height); c.setLineDash([]);
    } else {
      const glow = c.createRadialGradient(x + h.width / 2, y + h.height / 2, 10, x + h.width / 2, y + h.height / 2, Math.max(h.width, h.height) / 1.6);
      glow.addColorStop(0, '#ff8a2a'); glow.addColorStop(0.6, '#a8320c'); glow.addColorStop(1, '#3a1206');
      c.fillStyle = glow; c.fillRect(x, y, h.width, h.height);
      c.strokeStyle = '#ffb25a'; c.lineWidth = 4; c.setLineDash([10, 10]); c.strokeRect(x, y, h.width, h.height); c.setLineDash([]);
    }
  }
  // Lakes and ponds are overlapping slabs: shore first, then water, so the
  // slabs merge into one rounded body without seams.
  const water = obstacles.filter(o => o.material === 'water');
  if (water.length) {
    c.fillStyle = '#8c7a4e';
    for (const o of water) { const w = o.width ?? 0, h = o.height ?? 0; c.beginPath(); c.roundRect(o.x - ox - 12, o.y - oy - 12, w + 24, h + 24, Math.min(w, h) / 2 + 12); c.fill(); }
    c.fillStyle = '#1d5a7a';
    for (const o of water) { const w = o.width ?? 0, h = o.height ?? 0; c.beginPath(); c.roundRect(o.x - ox, o.y - oy, w, h, Math.min(w, h) / 2); c.fill(); }
    c.globalAlpha = 0.55; c.fillStyle = pattern('water');
    for (const o of water) { const w = o.width ?? 0, h = o.height ?? 0; c.beginPath(); c.roundRect(o.x - ox, o.y - oy, w, h, Math.min(w, h) / 2); c.fill(); }
    c.globalAlpha = 1;
  }
  c.fillStyle = 'rgba(0,0,0,.38)';
  for (const o of obstacles) {
    const w = o.width ?? 0, h = o.height ?? 0;
    if (o.material === 'water') continue;
    // Sprites get round shadows; a rectangle would show around their edges.
    if (['tree', 'rock', 'barrel', 'pine', 'deadtree', 'ice', 'cliff'].includes(o.material ?? '')) {
      const r = o.material === 'tree' || o.material === 'pine' ? 0.62 : 0.5;
      c.beginPath(); c.ellipse(o.x - ox + w / 2 + 8, o.y - oy + h / 2 + 10, w * r, h * r, 0, 0, Math.PI * 2); c.fill();
    } else if (o.material !== 'glass') c.fillRect(o.x - ox + 8, o.y - oy + 10, w, h);
  }
  for (const o of obstacles) {
    const w = o.width ?? 0, h = o.height ?? 0, x = o.x - ox, y = o.y - oy;
    const pick = hash2(Math.round(o.x), Math.round(o.y), 17);
    switch (o.material) {
      case 'water': break;
      case 'pine': {
        // Tree sprite, darkened to conifer green, with a snow cap.
        const s = w * 1.25;
        c.drawImage(sprite('tree')!, x + w / 2 - s / 2, y + h / 2 - s / 2, s, s);
        c.fillStyle = 'rgba(10,40,30,.35)'; c.beginPath(); c.arc(x + w / 2, y + h / 2, s * 0.42, 0, Math.PI * 2); c.fill();
        c.fillStyle = 'rgba(245,250,255,.85)';
        for (let n = 0; n < 5; n++) { c.beginPath(); c.arc(x + w / 2 + (hash2(n, o.x, 61) - 0.5) * s * 0.5, y + h / 2 + (hash2(o.y, n, 63) - 0.5) * s * 0.5, s * 0.07, 0, Math.PI * 2); c.fill(); }
        break;
      }
      case 'deadtree': {
        const cx = x + w / 2, cy = y + h / 2;
        c.strokeStyle = '#4a3a26'; c.lineCap = 'round';
        for (let n = 0; n < 5; n++) {
          const a = pick * 6 + n * 1.3;
          c.lineWidth = 7 - n; c.beginPath(); c.moveTo(cx, cy); c.lineTo(cx + Math.cos(a) * w * 0.6, cy + Math.sin(a) * h * 0.6); c.stroke();
        }
        c.fillStyle = '#3a2d1c'; c.beginPath(); c.arc(cx, cy, w * 0.18, 0, Math.PI * 2); c.fill();
        c.lineCap = 'butt';
        break;
      }
      case 'ice': {
        c.fillStyle = '#bfe3f2'; c.beginPath(); c.roundRect(x, y, w, h, 10); c.fill();
        c.fillStyle = 'rgba(255,255,255,.7)'; c.beginPath(); c.moveTo(x + 8, y + 8); c.lineTo(x + w * 0.6, y + 8); c.lineTo(x + 8, y + h * 0.6); c.fill();
        c.strokeStyle = '#7fb6cf'; c.lineWidth = 3; c.stroke(); c.beginPath(); c.roundRect(x, y, w, h, 10); c.stroke();
        break;
      }
      case 'cliff': {
        c.fillStyle = pattern('stone_c', 0.5); c.beginPath(); c.roundRect(x, y, w, h, Math.min(w, h) * 0.3); c.fill();
        c.fillStyle = 'rgba(40,34,28,.45)'; c.fill();
        c.fillStyle = 'rgba(255,255,255,.12)'; c.beginPath(); c.roundRect(x + 6, y + 6, w * 0.55, h * 0.4, 12); c.fill();
        c.strokeStyle = '#2a241c'; c.lineWidth = 4; c.beginPath(); c.roundRect(x, y, w, h, Math.min(w, h) * 0.3); c.stroke();
        break;
      }
      case 'reeds': {
        c.fillStyle = 'rgba(70,90,30,.55)'; c.fillRect(x, y, w, h);
        c.strokeStyle = '#9aa84a'; c.lineWidth = 2;
        for (let d = 4; d < w; d += 9) { c.beginPath(); c.moveTo(x + d, y + h); c.lineTo(x + d + 4, y); c.stroke(); }
        break;
      }
      case 'tree': {
        const s = w * 1.3;
        c.drawImage(sprite(pick < 0.2 ? 'tree_autumn' : 'tree')!, x + w / 2 - s / 2, y + h / 2 - s / 2, s, s);
        break;
      }
      case 'crate': case 'rock': case 'barrel': {
        const name: TileName = o.material === 'crate' ? (w < 60 ? 'crate_small' : 'crate')
          : o.material === 'rock' ? (pick < 0.5 ? 'rock' : 'rock_b') : (pick < 0.6 ? 'barrel' : 'barrel_grey');
        const inset = o.material === 'crate' ? 0 : -Math.min(w, h) * 0.12;
        c.drawImage(sprite(name)!, x + inset, y + inset, w - inset * 2, h - inset * 2);
        break;
      }
      case 'container': {
        c.fillStyle = containerColors[Math.floor(pick * containerColors.length)];
        c.fillRect(x, y, w, h);
        c.strokeStyle = 'rgba(0,0,0,.28)'; c.lineWidth = 3;
        const long = w > h;
        for (let d = 10; d < (long ? w : h) - 6; d += 14) {
          c.beginPath();
          if (long) { c.moveTo(x + d, y + 4); c.lineTo(x + d, y + h - 4); } else { c.moveTo(x + 4, y + d); c.lineTo(x + w - 4, y + d); }
          c.stroke();
        }
        c.strokeStyle = '#1a1a1a'; c.lineWidth = 3; c.strokeRect(x, y, w, h);
        break;
      }
      case 'glass':
        c.fillStyle = 'rgba(127,179,200,0.35)'; c.fillRect(x, y, w, h);
        c.strokeStyle = '#9fd4e8'; c.lineWidth = 3; c.strokeRect(x, y, w, h);
        break;
      default: {
        const skins: Record<string, [TileName, string, string | null]> = {
          brick: ['brick', '#4a2216', null],
          metal: ['stone_c', '#223443', 'rgba(40,64,90,.35)'],
          sandbag: ['tanbrick', '#5e4a26', null],
          hedge: ['grass_c', '#173d17', 'rgba(0,40,0,.35)'],
          wall: ['stone_b', '#2f403a', 'rgba(20,30,28,.2)'],
        };
        const [tile, stroke, tint] = skins[o.material ?? 'wall'] ?? skins.wall;
        c.fillStyle = pattern(tile, 0.5); c.fillRect(x, y, w, h);
        if (tint) { c.fillStyle = tint; c.fillRect(x, y, w, h); }
        c.strokeStyle = stroke; c.lineWidth = 4; c.strokeRect(x, y, w, h);
      }
    }
  }
  return true;
}
