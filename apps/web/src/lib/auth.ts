import { createClient, type User } from '@workos-inc/authkit-js';

type AuthClient = Awaited<ReturnType<typeof createClient>>;

let client: AuthClient | null = null;

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

export async function initializeAuth(): Promise<User | null> {
  const clientId = import.meta.env.VITE_WORKOS_CLIENT_ID;
  if (!clientId) return null;
  client = await createClient(clientId, { redirectUri: window.location.origin });
  return client.getUser();
}

export async function signIn() {
  if (!client) throw new Error('WorkOS sign-in is not configured; set VITE_WORKOS_CLIENT_ID and rebuild the web app');
  await client.signIn();
}

export function signOut() {
  if (!client) return;
  client.signOut({ returnTo: window.location.origin });
}

export async function accessToken(): Promise<string> {
  if (!client?.getUser()) return '';
  return client.getAccessToken();
}
