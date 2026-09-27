import { describe, expect, it } from 'vitest';
import { matchHref, matchRosterLabel } from './matchNavigation';

it('opens Rust results in the V2 replay workspace instead of the legacy recap', () => {
  expect(matchHref({ matchId: 'rust-run', engineVersion: 4, status: 'finished' })).toBe('#/v2/rust-run');
  expect(matchHref({ matchId: 'rust-run', engineVersion: 4, status: 'running' })).toBe('#/v2/rust-run');
});

it('preserves legacy recap and live routes', () => {
  expect(matchHref({ matchId: 'classic', status: 'finished' })).toBe('#/match/classic/detail');
  expect(matchHref({ matchId: 'classic', status: 'running' })).toBe('#/match/classic');
});

it('treats a null roster as an open lobby', () => {
  expect(matchRosterLabel({ robots: null as unknown as [] })).toBe('Open lobby · robots join before the match starts');
});

describe('roster label', () => {
  it('lists players first and counts bots', () => {
    const robots = [
      { displayName: 'Game Bot 1', bot: true }, { displayName: 'Ada' }, { displayName: 'Game Bot 2', bot: true },
    ] as unknown as Parameters<typeof matchRosterLabel>[0]['robots'];
    expect(matchRosterLabel({ robots })).toBe('Ada · 2 bots');
  });
});
