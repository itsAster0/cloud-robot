export type RouteName =
  | 'home' | 'play' | 'match' | 'match-detail' | 'matches' | 'box'
  | 'sdk' | 'api-docs' | 'profile' | 'leaderboard' | 'create' | 'settings'
  | 'spectate' | 'tournaments' | 'admin' | 'not-found';

export interface Route {
  name: RouteName;
  path: string;
  parameter?: string;
}

export function parseRoute(hash: string): Route {
  const path = decodeURIComponent(hash.replace(/^#/, '') || '/').replace(/\/+$/, '') || '/';
  const matchDetail = path.match(/^\/match\/([^/]+)\/detail$/);
  if (matchDetail) return { name: 'match-detail', path, parameter: matchDetail[1] };
  const match = path.match(/^\/match\/([^/]+)$/);
  if (match) return { name: 'match', path, parameter: match[1] };
  const profile = path.match(/^\/profile\/([^/]+)$/);
  if (profile) return { name: 'profile', path, parameter: profile[1] };
  const routes: Record<string, RouteName> = {
    '/': 'home', '/play': 'play', '/matches': 'matches', '/box': 'box',
    '/docs/sdk': 'sdk', '/docs/api': 'api-docs', '/leaderboard': 'leaderboard',
    '/create': 'create', '/settings': 'settings', '/spectate': 'spectate',
    '/tournaments': 'tournaments', '/admin': 'admin', '/arena': 'play',
  };
  return { name: routes[path] ?? 'not-found', path };
}

export function routeHref(path: string) {
  return `#${path}`;
}
