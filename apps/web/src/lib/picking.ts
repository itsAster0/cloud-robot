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
