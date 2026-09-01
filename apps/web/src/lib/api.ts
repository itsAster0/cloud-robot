import type { AdminStatus, CloudStatus, Match, PlayerStats, QueueStatus, Replay, RobotBox, RobotEnrollmentResponse, ScriptTemplate, ScriptVersion, Team } from './types';

let tokenProvider: (() => Promise<string>) | null = null;

export function setTokenProvider(provider: (() => Promise<string>) | null) {
  tokenProvider = provider;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let authorization: Record<string, string> = {};
  if (tokenProvider) {
    const token = await tokenProvider();
    if (token) authorization = { Authorization: `Bearer ${token}` };
  }
  const response = await fetch(path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...authorization, ...init?.headers },
  });
  const body = (await response.json()) as T & { error?: string };
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}

export interface ListedMatch extends Match {
  viewers?: number;
}

export const api = {
  cloudStatus: () => request<CloudStatus>('/api/cloud/status'),
  createMatch: (input?: { mode?: string; mapId?: string; arenaWidth?: number; arenaHeight?: number; practice?: boolean; bots?: number; botDifficulty?: string; friendlyFire?: boolean; regenPerTick?: number; rammingDamage?: boolean; botPersonality?: string }) => request<Match>('/api/matches', { method: 'POST', body: input ? JSON.stringify(input) : undefined }),
  listMatches: (status?: string, limit = 50) => request<{ matches: ListedMatch[] }>(`/api/matches?limit=${limit}${status ? `&status=${encodeURIComponent(status)}` : ''}`),
  getMatch: (matchId: string) => request<Match>(`/api/matches/${encodeURIComponent(matchId)}`),
  getReplay: (matchId: string) => request<Replay>(`/api/matches/${encodeURIComponent(matchId)}/replay`),
  getProfile: (handle: string) => request<{ profile: PlayerStats; recentMatches: Match[] }>(`/api/profiles/${encodeURIComponent(handle)}`),
  getLeaderboard: (mode = 'duel', limit = 50) => request<{ mode: string; entries: { rank: number; player: PlayerStats; rating: number }[] }>(`/api/leaderboard?mode=${encodeURIComponent(mode)}&limit=${limit}`),
  queueStatus: () => request<QueueStatus>('/api/queue'),
  joinQueue: (input: { displayName: string; mode: 'duel'; runtime: 'lua5.4'; startCommand: string }) => request<QueueStatus>('/api/queue', { method: 'POST', body: JSON.stringify(input) }),
  leaveQueue: () => request<QueueStatus>('/api/queue', { method: 'DELETE' }),
  adminStatus: () => request<AdminStatus>('/api/admin/status'),
  submitRobot: (matchId: string, input: { displayName: string; team: Team; startCommand: string; runtime: string }) =>
    request<RobotEnrollmentResponse>(`/api/matches/${encodeURIComponent(matchId)}/robots`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  withdrawRobot: (matchId: string) =>
    request<Match>(`/api/matches/${encodeURIComponent(matchId)}/robots`, { method: 'DELETE' }),
  startMatch: (matchId: string) =>
    request<Match>(`/api/matches/${encodeURIComponent(matchId)}/start`, { method: 'POST' }),
  withdrawFromMatch: (matchId: string) =>
    request<{ status: string; robotId: string }>(`/api/matches/${encodeURIComponent(matchId)}/withdraw`, { method: 'POST' }),
  ensureBox: () => request<RobotBox>('/api/me/box', { method: 'POST' }),
  getBox: () => request<RobotBox>('/api/me/box'),
  getBoxMain: () => request<{ source: string }>('/api/me/box/main.lua'),
  writeBoxMain: (template: string) =>
    request<{ template: string; source: string }>('/api/me/box/main.lua', { method: 'PUT', body: JSON.stringify({ template }) }),
  listScripts: () => request<{ scripts: ScriptTemplate[] }>('/api/scripts'),
  listScriptVersions: () => request<{ versions: ScriptVersion[] }>('/api/me/box/scripts'),
  restoreScriptVersion: (versionId: string) => request<{ versionId: string; source: string }>(`/api/me/box/scripts/${encodeURIComponent(versionId)}/restore`, { method: 'POST' }),
  setBoxKey: (publicKey: string) => request<RobotBox>('/api/me/box/ssh-key', { method: 'PUT', body: JSON.stringify({ publicKey }) }),
  restartBox: () => request<RobotBox>('/api/me/box/restart', { method: 'POST' }),
  releaseBox: () => request<{ status: string }>('/api/me/box/release', { method: 'POST' }),
};
