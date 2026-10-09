import { createClient, type User } from '@workos-inc/authkit-js';

type AuthClient = Awaited<ReturnType<typeof createClient>>;

let client: AuthClient | null = null;

// Local accounts (LOCAL_AUTH_ENABLED on the API) keep their session token in
// localStorage; WorkOS manages its own session.
const LOCAL_SESSION_KEY = 'robot-arena:local-session';
type LocalSession = { token: string; expiresAt: string; user: { id: string; username: string } };

function readLocalSession(): LocalSession | null {
  try {
    const raw = localStorage.getItem(LOCAL_SESSION_KEY);
    if (!raw) return null;
    const session = JSON.parse(raw) as LocalSession;
    if (!session.token || Date.parse(session.expiresAt) <= Date.now()) {
      localStorage.removeItem(LOCAL_SESSION_KEY);
      return null;
    }
    return session;
  } catch {
    return null;
  }
}

// The app reads only id, email, and firstName from a user, so a local account
// is presented in the WorkOS shape rather than threading a second type through.
function localUser(session: LocalSession): User {
  return { id: session.user.id, email: session.user.username, firstName: session.user.username, lastName: null } as unknown as User;
}

export function authConfigured(): boolean {
  return Boolean(import.meta.env.VITE_WORKOS_CLIENT_ID);
}

export function redirectCallbackPending(): boolean {
  const params = new URLSearchParams(window.location.search);
  return params.has('code') || params.has('error');
}

export function clearRedirectCallback() {
  const url = new URL(window.location.href);
  for (const param of ['code', 'state', 'error', 'error_description']) {
    url.searchParams.delete(param);
  }
  history.replaceState({}, '', url.toString());
}

async function workosClient(): Promise<AuthClient | null> {
  const clientId = import.meta.env.VITE_WORKOS_CLIENT_ID;
  if (!clientId) return null;
  client ??= await createClient(clientId, { redirectUri: window.location.origin });
  return client;
}

export async function initializeAuth(): Promise<User | null> {
  const local = readLocalSession();
  if (local) return localUser(local);
  const workos = await workosClient();
  return workos ? workos.getUser() : null;
}

export async function signIn() {
  const workos = await workosClient();
  if (!workos) throw new Error('WorkOS sign-in is not configured; set VITE_WORKOS_CLIENT_ID and rebuild the web app');
  await workos.signIn();
}

export async function authProviders(): Promise<{ local: boolean; workos: boolean }> {
  try {
    const response = await fetch('/api/auth/providers');
    if (response.ok) return await response.json();
  } catch { /* fall back to WorkOS only */ }
  return { local: false, workos: authConfigured() };
}

export async function localSignIn(mode: 'login' | 'register', username: string, password: string) {
  let response: Response;
  try {
    response = await fetch(`/api/auth/local/${mode}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
  } catch {
    throw new Error('Cannot reach the arena server. Try again in a few seconds.');
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `Sign-in failed (${response.status})`);
  localStorage.setItem(LOCAL_SESSION_KEY, JSON.stringify(body as LocalSession));
}

export function signOut() {
  if (readLocalSession()) {
    localStorage.removeItem(LOCAL_SESSION_KEY);
    window.location.reload();
    return;
  }
  client?.signOut({ returnTo: window.location.origin });
}

export async function accessToken(): Promise<string> {
  const local = readLocalSession();
  if (local) return local.token;
  if (!client?.getUser()) return '';
  return client.getAccessToken();
}
