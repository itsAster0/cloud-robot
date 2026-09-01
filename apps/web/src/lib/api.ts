import type { CloudStatus, Match, RobotBox, RobotEnrollmentResponse, ScriptTemplate, Team } from './types';

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

export const api = {
  cloudStatus: () => request<CloudStatus>('/api/cloud/status'),
  createMatch: () => request<Match>('/api/matches', { method: 'POST' }),
  getMatch: (matchId: string) => request<Match>(`/api/matches/${encodeURIComponent(matchId)}`),
  submitRobot: (matchId: string, input: { displayName: string; team: Team; startCommand: string; runtime: string }) =>
    request<RobotEnrollmentResponse>(`/api/matches/${encodeURIComponent(matchId)}/robots`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  withdrawRobot: (matchId: string) =>
    request<Match>(`/api/matches/${encodeURIComponent(matchId)}/robots`, { method: 'DELETE' }),
  startMatch: (matchId: string) =>
    request<Match>(`/api/matches/${encodeURIComponent(matchId)}/start`, { method: 'POST' }),
  ensureBox: () => request<RobotBox>('/api/me/box', { method: 'POST' }),
  getBox: () => request<RobotBox>('/api/me/box'),
  getBoxMain: () => request<{ source: string }>('/api/me/box/main.lua'),
  writeBoxMain: (template: string) =>
    request<{ template: string; source: string }>('/api/me/box/main.lua', { method: 'PUT', body: JSON.stringify({ template }) }),
  listScripts: () => request<{ scripts: ScriptTemplate[] }>('/api/scripts'),
  setBoxKey: (publicKey: string) => request<RobotBox>('/api/me/box/ssh-key', { method: 'PUT', body: JSON.stringify({ publicKey }) }),
  restartBox: () => request<RobotBox>('/api/me/box/restart', { method: 'POST' }),
};
