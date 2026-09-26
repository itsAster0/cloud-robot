import type { Match } from './types';

export function matchHref(match: Pick<Match, 'matchId' | 'engineVersion' | 'status'>): string {
  const id = encodeURIComponent(match.matchId);
  if (match.engineVersion === 4) return `#/v2/${id}`;
  return `#/match/${id}${match.status === 'finished' ? '/detail' : ''}`;
}

export function matchModeLabel(mode: string): string {
  const names: Record<string, string> = {
    'br-solo': 'Solo battle royale', 'br-squad': 'Squad battle royale',
    'quick-duel': 'Quick duel', sandbox: 'Sandbox', duel: 'Duel', solo: 'Solo practice', squad: 'Squad practice',
  };
  return names[mode] ?? mode.replace(/[-_]/g, ' ');
}

export function matchRosterLabel(match: Pick<Match, 'robots'>): string {
  const names = match.robots.slice(0, 3).map(robot => robot.displayName);
  if (!names.length) return 'Open lobby · robots join before the match starts';
  const remaining = match.robots.length - names.length;
  return `${names.join(' · ')}${remaining > 0 ? ` · +${remaining} more` : ''}`;
}
