import { describe, expect, it } from 'vitest';
import { pickRobot, screenToWorld } from './picking';

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
