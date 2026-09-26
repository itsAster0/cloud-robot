export type WorkspacePanel = 'code' | 'build' | 'match' | 'debug' | 'results';

export type RouteName =
  | 'v2' | 'workspace' | 'home' | 'play' | 'match' | 'match-detail' | 'matches' | 'box'
  | 'sdk' | 'api-docs' | 'profile' | 'leaderboard' | 'create' | 'settings'
  | 'spectate' | 'tournaments' | 'admin' | 'not-found';

export interface Route {
  name: RouteName;
  path: string;
  parameter?: string;
  panel?: WorkspacePanel;
}

export function parseRoute(hash: string): Route {
  const rawPath = hash.replace(/^#/, '') || '/';
  let path: string;
  try {
    path = decodeURIComponent(rawPath).replace(/\/+$/, '') || '/';
  } catch {
    return { name: 'not-found', path: rawPath };
  }
  const workspacePanels: Record<string, WorkspacePanel> = {
    '/workspace': 'code', '/workspace/code': 'code', '/workspace/build': 'build',
    '/workspace/matches': 'match', '/workspace/match': 'match',
    '/workspace/test': 'debug', '/workspace/debug': 'debug', '/workspace/results': 'results',
  };
  if (workspacePanels[path]) return { name: 'workspace', path, panel: workspacePanels[path] };
  const v2 = path.match(/^\/v2(?:\/([^/]+))?$/);
  if (v2) return {name:'v2',path,parameter:v2[1]};
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
