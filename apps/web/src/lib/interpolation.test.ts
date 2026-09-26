import { describe, expect, it } from 'vitest';
import { angleBetween, SnapshotBuffer } from './interpolation';
import type { Snapshot } from './types';
const snap = (tick: number, matchId = 'a'): Snapshot => ({ type: 'snapshot', version: 3, sequence: tick, tick, matchId, status: 'running', robots: [], projectiles: [] });
describe('snapshot playback', () => {
  it('takes the short path through north', () => { expect(angleBetween(359, 1, .5)).toBe(360); expect(angleBetween(1, 359, .5)).toBe(0); });
  it('interpolates on server ticks and freezes on prolonged gaps', () => {
    const b = new SnapshotBuffer(); b.push(snap(1), 0); b.push(snap(2), 100); b.push(snap(3), 200);
    const s = b.sample(200)!; expect(s.before.tick).toBe(1); expect(s.after.tick).toBe(2); expect(s.amount).toBeCloseTo(.5);
    expect(b.sample(1000)!.after.tick).toBe(3);
  });
  it('rejects old snapshots and resets for a different match', () => {
    const b = new SnapshotBuffer(); b.push(snap(8), 0); b.push(snap(7), 10); expect(b.sample(1000)!.after.tick).toBe(8);
    b.push(snap(0, 'b'), 1100); expect(b.sample(1100)!.after.matchId).toBe('b');
  });
});

describe('uneven delivery recovery',()=>{
 it('never rewinds with jitter, missing packets and duplicates',()=>{
  const b=new SnapshotBuffer(); let position=-Infinity;
  for(const [tick,at] of [[0,0],[2,110],[6,350],[6,360],[8,470],[12,800]]){
   b.push({...snap(tick),tickRate:20},at);
   const s=b.sample(at)!;const current=s.before.tick+(s.after.tick-s.before.tick)*s.amount;
   expect(current).toBeGreaterThanOrEqual(position); position=current;
  }
 });
 it('rebaselines after a hidden-tab gap without replaying stale motion',()=>{
  const b=new SnapshotBuffer();b.push(snap(1),0);b.push(snap(2),100);b.sample(150);
  b.push(snap(100),10000);const s=b.sample(10000)!;
  expect(s.before.tick).toBe(100);expect(s.after.tick).toBe(100);
 });
});
