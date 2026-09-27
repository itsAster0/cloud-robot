import { describe, expect, it } from 'vitest';
import { pickRobot, pickThing, screenToWorld } from './picking';

describe('robot picking', () => {
  const camera = { x: 1000, y: 500, zoom: 2, width: 800, height: 400 };

  it('maps the canvas centre to the camera position', () => {
    expect(screenToWorld(camera, 400, 200)).toEqual({ x: 1000, y: 500 });
    expect(screenToWorld(camera, 600, 300)).toEqual({ x: 1100, y: 550 });
  });

  it('picks the nearest robot within reach', () => {
    const robots = [{ robotId: 'a', x: 1010, y: 500 }, { robotId: 'b', x: 1004, y: 503 }];
    expect(pickRobot(robots, 1000, 500, 1)).toBe('b');
    expect(pickRobot(robots, 1200, 500, 1)).toBe('');
  });

  it('widens the reach when zoomed out', () => {
    const robots = [{ robotId: 'far', x: 1100, y: 500 }];
    expect(pickRobot(robots, 1000, 500, 1)).toBe('');
    expect(pickRobot(robots, 1000, 500, 0.1)).toBe('far');
  });
});

describe('pickThing', () => {
  const layers = {
    items: [{ itemId: 'loot-1', type: 'container', x: 100, y: 100, active: true, spawnTick: 0, pickupRadius: 35 }],
    transit: [{ id: 't', x: 500, y: 500, targetX: 900, targetY: 900 }],
    obstacles: [{ id: 'lake', shape: 'aabb' as const, x: 1000, y: 0, width: 200, height: 200, material: 'water' }],
    hazards: [{ id: 'bog', kind: 'slow', x: 2000, y: 0, width: 300, height: 200 }],
    sites: [{ kind: 'armoury', x: 3000, y: 3000, biome: 'snow' }],
  };
  it('prefers loot near the click', () => expect(pickThing(layers, 110, 95, 1).kind).toBe('item'));
  it('finds transit pads, obstacles, hazards, and sites', () => {
    expect(pickThing(layers, 505, 490, 1).kind).toBe('transit');
    expect(pickThing(layers, 1100, 100, 1)).toMatchObject({ kind: 'obstacle', obstacle: { material: 'water' } });
    expect(pickThing(layers, 2100, 100, 1)).toMatchObject({ kind: 'hazard' });
    expect(pickThing(layers, 3100, 3000, 1)).toMatchObject({ kind: 'site' });
  });
  it('falls back to the ground under the pointer', () => expect(pickThing(layers, 5000, 5000, 1)).toEqual({ kind: 'ground', x: 5000, y: 5000 }));
  it('widens loot reach when zoomed out', () => expect(pickThing(layers, 150, 100, 0.2).kind).toBe('item'));
});
