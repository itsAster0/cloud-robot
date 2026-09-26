import { describe, expect, it } from 'vitest';
import { aliveRobots, elapsedLabel, hpFraction, killsLeader, teamColor, teamStandings, weaponGlyph } from './matchStats';
import type { RobotState } from './types';

function robot(over: Partial<RobotState> & { robotId: string }): RobotState {
  return {
    name: over.robotId, team: 'red', x: 0, y: 0, heading: 0, hp: 100,
    cooldown: 0, alive: true, failed: false, connected: true,
    avgResponseMs: 0, lastResponseMs: 0, computeMs: 0, memoryMb: 0, ...over,
  };
}

describe('matchStats', () => {
  it('colors red and blue teams distinctly and hashes the rest', () => {
    expect(teamColor('red')).not.toBe(teamColor('blue'));
    expect(teamColor('squad-00')).toBe(teamColor('squad-00'));
  });

  it('maps every catalog weapon to a glyph', () => {
    for (const w of ['plasma', 'machine_gun', 'shotgun', 'cannon', 'railgun', 'grenade', 'incendiary', 'cryo', 'emp']) {
      expect(weaponGlyph(w)).not.toBe('○');
    }
    expect(weaponGlyph(undefined)).toBe('○');
  });

  it('finds the kills leader and counts the living', () => {
    const robots = [robot({ robotId: 'a', kills: 1 }), robot({ robotId: 'b', kills: 3 }), robot({ robotId: 'c', kills: 0, alive: false })];
    expect(killsLeader(robots)?.robotId).toBe('b');
    expect(aliveRobots(robots)).toHaveLength(2);
    expect(killsLeader([robot({ robotId: 'z' })])).toBeNull();
  });

  it('ranks team standings by survivors then kills', () => {
    const robots = [
      robot({ robotId: 'a', team: 'red', kills: 5 }),
      robot({ robotId: 'b', team: 'red', alive: false }),
      robot({ robotId: 'c', team: 'blue', kills: 1 }),
    ];
    const [first, second] = teamStandings(robots);
    expect(first.team).toBe('red');
    expect(first.alive).toBe(1);
    expect(second.team).toBe('blue');
  });

  it('formats elapsed time and hp fractions', () => {
    expect(elapsedLabel(1200)).toBe('1:00');
    expect(hpFraction(robot({ robotId: 'a', hp: 25, maxHp: 100 }))).toBe(0.25);
  });
});
