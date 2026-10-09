import type { RobotState } from './types';

const FFA_PALETTE = ['#dfff86', '#c79bff', '#ffc857', '#3fe0c5', '#ff5bd7', '#8affda', '#ff9d5c', '#7dd8ff'];

export function teamColor(team: string): string {
  if (team === 'red') return '#ff8070';
  if (team === 'blue') return '#71c8ff';
  let hash = 0;
  for (let i = 0; i < team.length; i++) hash = (hash * 31 + team.charCodeAt(i)) >>> 0;
  return FFA_PALETTE[hash % FFA_PALETTE.length];
}

const WEAPON_GLYPHS: Record<string, string> = {
  plasma: '◉',
  machine_gun: '⋮',
  shotgun: '∴',
  cannon: '●',
  railgun: '┃',
  grenade: '✸',
  incendiary: '♨',
  cryo: '❄',
  emp: '⚡',
};

export function weaponGlyph(weapon?: string): string {
  return (weapon && WEAPON_GLYPHS[weapon]) || '○';
}

export function weaponLabel(weapon?: string): string {
  return (weapon || 'unknown').replace(/_/g, ' ');
}

export function aliveRobots(robots: RobotState[]): RobotState[] {
  return robots.filter((r) => r.alive);
}

export function killsLeader(robots: RobotState[]): RobotState | null {
  let best: RobotState | null = null;
  for (const r of robots) {
    if ((r.kills ?? 0) > (best?.kills ?? 0)) best = r;
  }
  return best && (best.kills ?? 0) > 0 ? best : null;
}

export interface TeamStanding {
  team: string;
  alive: number;
  total: number;
  kills: number;
}

export function teamStandings(robots: RobotState[]): TeamStanding[] {
  const map = new Map<string, TeamStanding>();
  for (const r of robots) {
    const entry = map.get(r.team) ?? { team: r.team, alive: 0, total: 0, kills: 0 };
    entry.total += 1;
    if (r.alive) entry.alive += 1;
    entry.kills += r.kills ?? 0;
    map.set(r.team, entry);
  }
  return [...map.values()].sort((a, b) => b.alive - a.alive || b.kills - a.kills);
}

export function elapsedLabel(tick: number, tickRate = 20): string {
  const seconds = Math.floor(tick / tickRate);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}

export function hpFraction(robot: RobotState): number {
  return Math.max(0, Math.min(1, robot.hp / (robot.maxHp ?? 100)));
}

export const siteColor: Record<string, string> = {
  repair_depot: '#3fe0c5',
  armoury: '#ff9d5c',
  relay_station: '#71c8ff',
  transit_station: '#b58dff',
  bunker: '#8a8f96',
  exposed_supply: '#efc86b',
};

export function siteLabel(kind: string): string {
  return kind.replace(/_/g, ' ');
}
