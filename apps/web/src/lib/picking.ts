import type { ArenaItem, ArenaObstacle, WorldHazard, WorldSite } from './types';

export interface Camera { x: number; y: number; zoom: number; width: number; height: number }
export interface Placed { robotId: string; x: number; y: number }

/** Canvas CSS pixel to world coordinates for a camera centred on (x, y). */
export function screenToWorld(camera: Camera, px: number, py: number) {
  return { x: camera.x + (px - camera.width / 2) / camera.zoom, y: camera.y + (py - camera.height / 2) / camera.zoom };
}

/**
 * Nearest robot to a world point. The reach covers the whole 36-unit tank
 * sprite and never shrinks below 16 screen pixels, so moving or zoomed-out
 * robots stay clickable. Returns '' when nothing is close enough.
 */
export function pickRobot(robots: Placed[], x: number, y: number, zoom: number) {
  const reach = Math.max(32, 16 / zoom);
  let best = '', bestDistance = reach;
  for (const r of robots) {
    const d = Math.hypot(r.x - x, r.y - y);
    if (d < bestDistance) { best = r.robotId; bestDistance = d; }
  }
  return best;
}


/** A non-robot thing on the map the viewer can inspect. */
export type Picked =
  | { kind: 'item'; item: ArenaItem }
  | { kind: 'transit'; transit: { id: string; x: number; y: number; targetX: number; targetY: number } }
  | { kind: 'hazard'; hazard: WorldHazard }
  | { kind: 'obstacle'; obstacle: ArenaObstacle }
  | { kind: 'site'; site: WorldSite }
  | { kind: 'ground'; x: number; y: number };

export interface PickLayers {
  items?: ArenaItem[];
  transit?: { id: string; x: number; y: number; targetX: number; targetY: number }[];
  hazards?: WorldHazard[];
  obstacles?: ArenaObstacle[];
  sites?: WorldSite[];
}

const inside = (x: number, y: number, o: { x: number; y: number; width?: number; height?: number }, pad = 0) =>
  x >= o.x - pad && x <= o.x + (o.width ?? 0) + pad && y >= o.y - pad && y <= o.y + (o.height ?? 0) + pad;

/**
 * What sits under a world point, most specific first: loot, transit pads,
 * obstacles, hazards, then the site plaza, else plain ground. Small things get
 * a reach of at least 12 screen pixels so they stay clickable zoomed out.
 */
export function pickThing(layers: PickLayers, x: number, y: number, zoom: number): Picked {
  const reach = Math.max(28, 14 / zoom);
  let item: ArenaItem | undefined, itemDistance = reach;
  for (const i of layers.items ?? []) {
    const d = Math.hypot(i.x - x, i.y - y);
    if (i.active !== false && d < itemDistance) { item = i; itemDistance = d; }
  }
  if (item) return { kind: 'item', item };
  const pad = Math.max(40, 12 / zoom);
  const transit = (layers.transit ?? []).find(t => Math.hypot(t.x - x, t.y - y) < pad);
  if (transit) return { kind: 'transit', transit };
  // Later obstacles are drawn on top, so search from the end.
  const obstacles = layers.obstacles ?? [];
  for (let n = obstacles.length - 1; n >= 0; n--) if (inside(x, y, obstacles[n])) return { kind: 'obstacle', obstacle: obstacles[n] };
  const hazard = (layers.hazards ?? []).find(h => inside(x, y, h));
  if (hazard) return { kind: 'hazard', hazard };
  const site = (layers.sites ?? []).find(s => Math.hypot(s.x - x, s.y - y) < 260);
  if (site) return { kind: 'site', site };
  return { kind: 'ground', x, y };
}

/** World-space centre of a picked thing, for highlighting and camera moves. */
export function pickedCentre(p: Picked): { x: number; y: number; r: number } {
  switch (p.kind) {
    case 'item': return { x: p.item.x, y: p.item.y, r: 22 };
    case 'transit': return { x: p.transit.x, y: p.transit.y, r: 40 };
    case 'hazard': return { x: p.hazard.x + p.hazard.width / 2, y: p.hazard.y + p.hazard.height / 2, r: Math.hypot(p.hazard.width, p.hazard.height) / 2 };
    case 'obstacle': { const w = p.obstacle.width ?? 0, h = p.obstacle.height ?? 0; return { x: p.obstacle.x + w / 2, y: p.obstacle.y + h / 2, r: Math.hypot(w, h) / 2 + 6 }; }
    case 'site': return { x: p.site.x, y: p.site.y, r: 270 };
    case 'ground': return { x: p.x, y: p.y, r: 30 };
  }
}
