export interface Camera { x: number; y: number; zoom: number; width: number; height: number }
export interface Placed { robotId: string; x: number; y: number }

/** Canvas CSS pixel to world coordinates for a camera centred on (x, y). */
export function screenToWorld(camera: Camera, px: number, py: number) {
  return { x: camera.x + (px - camera.width / 2) / camera.zoom, y: camera.y + (py - camera.height / 2) / camera.zoom };
}

/**
 * Nearest robot to a world point within a reach that never shrinks below
 * 14 screen pixels, so small robots stay clickable when zoomed out.
 * Returns '' when nothing is close enough.
 */
export function pickRobot(robots: Placed[], x: number, y: number, zoom: number) {
  const reach = Math.max(26, 14 / zoom);
  let best = '', bestDistance = reach;
  for (const r of robots) {
    const d = Math.hypot(r.x - x, r.y - y);
    if (d < bestDistance) { best = r.robotId; bestDistance = d; }
  }
  return best;
}
