import spritesUrl from './assets/robot-sprites.png';

// assets/robot-sprites.png: grayscale tank hull (83x78) then its barrel
// (24x58, centred in the next 83 px cell). Both face +y in the image.
const HULL = { sx: 0, sy: 0, w: 83, h: 78 };
const BARREL = { sx: 83 + 29, sy: 0, w: 25, h: 58 };
// World units: the hull spans about the robot's 28-unit collision diameter.
const HULL_WIDTH = 36;
const SCALE = HULL_WIDTH / HULL.w;

let image: HTMLImageElement | null = null;
let ready = false;
const tinted = new Map<string, HTMLCanvasElement>();

export function loadRobotSprites(onReady: () => void) {
  if (ready) return;
  if (!image) { image = new Image(); image.src = spritesUrl; }
  image.addEventListener('load', () => { ready = true; onReady(); }, { once: true });
}

/** The sprite sheet multiplied by `color`, keeping the sprite's alpha. */
function sheet(color: string) {
  let canvas = tinted.get(color);
  if (canvas || !image) return canvas;
  canvas = document.createElement('canvas');
  canvas.width = image.width; canvas.height = image.height;
  const c = canvas.getContext('2d')!;
  c.drawImage(image, 0, 0);
  c.globalCompositeOperation = 'multiply';
  c.fillStyle = color; c.fillRect(0, 0, canvas.width, canvas.height);
  c.globalCompositeOperation = 'destination-in';
  c.drawImage(image, 0, 0);
  if (tinted.size > 256) tinted.clear();
  tinted.set(color, canvas);
  return canvas;
}

/**
 * Draws a tank at (x, y): hull rotated to `heading`, barrel to `turret`
 * (degrees, 0 = +x). Returns false until the sprites load so callers can
 * fall back to vector shapes.
 */
export function drawRobot(c: CanvasRenderingContext2D, x: number, y: number, heading: number, turret: number, color: string) {
  const art = ready ? sheet(color) : undefined;
  if (!art) return false;
  c.save();
  c.translate(x, y);
  c.rotate((heading - 90) * Math.PI / 180);
  c.drawImage(art, HULL.sx, HULL.sy, HULL.w, HULL.h, -HULL.w * SCALE / 2, -HULL.h * SCALE / 2, HULL.w * SCALE, HULL.h * SCALE);
  c.restore();
  c.save();
  c.translate(x, y);
  c.rotate((turret - 90) * Math.PI / 180);
  // The barrel's base sits under the hull's turret dome.
  c.drawImage(art, BARREL.sx, BARREL.sy, BARREL.w, BARREL.h, -BARREL.w * SCALE / 2, -6 * SCALE, BARREL.w * SCALE, BARREL.h * SCALE);
  c.restore();
  return true;
}
