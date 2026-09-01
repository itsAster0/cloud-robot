import { describe, expect, it } from 'vitest';
import { isNewerSnapshot, type Snapshot } from './types';

function snapshot(sequence: number): Snapshot {
  return { type: 'snapshot', version: 1, matchId: 'm1', sequence, tick: sequence, status: 'running', robots: [], projectiles: [] };
}

describe('isNewerSnapshot', () => {
  it('rejects duplicate and older state', () => {
    expect(isNewerSnapshot(snapshot(4), snapshot(4))).toBe(false);
    expect(isNewerSnapshot(snapshot(4), snapshot(3))).toBe(false);
    expect(isNewerSnapshot(snapshot(4), snapshot(5))).toBe(true);
  });
});
