import { describe, expect, it } from 'vitest';
import { isNewerSnapshot, type Snapshot } from './types';

function snapshot(sequence: number): Snapshot {
  return { type: 'snapshot', version: 3, matchId: 'm1', sequence, tick: sequence, status: 'running', robots: [], projectiles: [] };
}

describe('isNewerSnapshot', () => {
  it('rejects duplicate and older state', () => {
    expect(isNewerSnapshot(snapshot(4), snapshot(4))).toBe(false);
    expect(isNewerSnapshot(snapshot(4), snapshot(3))).toBe(false);
    expect(isNewerSnapshot(snapshot(4), snapshot(5))).toBe(true);
  });

  it('carries protocol v3 arena fields', () => {
    const snap = snapshot(6);
    snap.mines = [{ mineId: 'mine-1', ownerId: 'r1', team: 'red', x: 10, y: 20, spawnTick: 0, armTick: 30, active: true }];
    snap.turrets = [{ turretId: 'turret-1', x: 30, y: 40, hp: 80, maxHp: 100, alive: true }];
    snap.zone = { active: true, x: 400, y: 250, radius: 180, damage: 2, stage: 3 };
    expect(snap.version).toBe(3);
    expect(isNewerSnapshot(snapshot(4), snap)).toBe(true);
    expect(snap.zone?.stage).toBe(3);
    expect(snap.mines?.[0].armTick).toBe(30);
    expect(snap.turrets?.[0].alive).toBe(true);
  });
});
