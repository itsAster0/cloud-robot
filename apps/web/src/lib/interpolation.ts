import type { Snapshot } from './types';

export function angleBetween(from: number, to: number, amount: number): number {
  return from + ((to - from + 540) % 360 - 180) * amount;
}

/** Server ticks define motion; arrival times only estimate the playback clock. */
export class SnapshotBuffer {
  private entries: { snapshot: Snapshot; time: number }[] = [];
  private arrival = 0;
  private delay = 150;
  private interval = 100;
  private cursor = -Infinity;

  push(snapshot: Snapshot, now: number) {
    const last = this.entries[this.entries.length - 1];
    if (last && last.snapshot.matchId === snapshot.matchId && snapshot.sequence <= last.snapshot.sequence) return;
    if (!last || last.snapshot.matchId !== snapshot.matchId || now - this.arrival > 2000) {
      this.entries = []; this.cursor = -Infinity; this.delay = 150;
    } else {
      const gap = now - this.arrival;
      this.delay = Math.max(100, Math.min(250, this.delay * .9 + Math.max(150, gap * 1.25) * .1));
    }
    this.interval = 1000 / (snapshot.tickRate ?? 10);
    this.entries.push({ snapshot, time: snapshot.tick * this.interval });
    this.entries = this.entries.slice(-12);
    this.arrival = now;
  }

  sample(now: number) {
    const latest = this.entries[this.entries.length - 1];
    if (!latest) return null;
    const renderTime = Math.max(this.cursor, Math.min(latest.time, latest.time + now - this.arrival - this.delay));
    this.cursor = renderTime;
    let before = this.entries[0], after = before;
    for (const entry of this.entries) {
      after = entry;
      if (entry.time >= renderTime) break;
      before = entry;
    }
    const amount = before === after ? 1 : Math.max(0, Math.min(1, (renderTime - before.time) / (after.time - before.time)));
    return { before: before.snapshot, after: after.snapshot, amount };
  }
}
