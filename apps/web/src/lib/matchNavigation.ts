import type { Match } from './types';

export function matchHref(match: Pick<Match, 'matchId' | 'engineVersion' | 'status'>): string {
  const id = encodeURIComponent(match.matchId);
  if (match.engineVersion === 4) return `#/v2/${id}`;
  return `#/match/${id}${match.status === 'finished' ? '/detail' : ''}`;
}

export function matchModeLabel(mode: string): string {
  const names: Record<string, string> = {
    'br-solo': 'Solo battle royale', 'br-squad': 'Squad battle royale',
    'quick-duel': 'Quick duel', sandbox: 'Sandbox', arena: 'The Arena', duel: 'Duel', solo: 'Solo practice', squad: 'Squad practice',
  };
  return names[mode] ?? mode.replace(/[-_]/g, ' ');
}

export function matchRosterLabel(match: Pick<Match, 'robots'>): string {
  // Older API responses encoded bot-only rosters as null.
  const robots = match.robots ?? [];
  if (!robots.length) return 'Open lobby · robots join before the match starts';
  // Players first; bots are summarised as a count.
  const humans = robots.filter(robot => !robot.bot);
  const bots = robots.length - humans.length;
  const names = humans.slice(0, 3).map(robot => robot.displayName);
  const more = humans.length - names.length;
  const parts = [...names, ...(more > 0 ? [`+${more} more players`] : []), ...(bots ? [`${bots} ${bots === 1 ? 'bot' : 'bots'}`] : [])];
  return parts.join(' · ');
}
