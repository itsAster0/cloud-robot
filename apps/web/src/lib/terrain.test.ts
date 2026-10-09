import { describe, expect, it } from 'vitest';
import { ATLAS, biomeAt, roadsFor, ROAD_HALF_WIDTH } from './terrain';

const sites = [
  { kind: 'armoury', x: 600, y: 375, biome: 'urban' },
  { kind: 'bunker', x: 1800, y: 375, biome: 'desert' },
  { kind: 'relay_station', x: 600, y: 1125, biome: 'forest' },
  { kind: 'transit_station', x: 1800, y: 1125, biome: 'industrial' },
];

describe('terrain', () => {
  it('maps each point to its nearest site biome', () => {
    expect(biomeAt(sites, 600, 375)).toBe('urban');
    expect(biomeAt(sites, 1800, 1125)).toBe('industrial');
    expect(biomeAt([], 10, 10)).toBe('forest');
    expect(biomeAt([{ kind: 'x', x: 0, y: 0, biome: 'unknown' }], 5, 5)).toBe('forest');
  });

  it('is deterministic for the same point', () => {
    expect(biomeAt(sites, 1190, 760)).toBe(biomeAt(sites, 1190, 760));
  });

  it('connects grid neighbours with axis-aligned roads', () => {
    const roads = roadsFor(sites);
    expect(roads).toHaveLength(4);
    expect(roads).toContainEqual({ x: 600, y: 375 - ROAD_HALF_WIDTH, width: 1200, height: ROAD_HALF_WIDTH * 2 });
    expect(roads).toContainEqual({ x: 600 - ROAD_HALF_WIDTH, y: 375, width: ROAD_HALF_WIDTH * 2, height: 750 });
  });

  it('matches the atlas image grid', () => {
    // 8 columns x 4 rows in assets/terrain-atlas.png.
    expect(ATLAS.length).toBeLessThanOrEqual(32);
    expect(new Set(ATLAS).size).toBe(ATLAS.length);
  });
});
