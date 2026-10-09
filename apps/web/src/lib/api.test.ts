import { afterEach, expect, it, vi } from 'vitest';
import { request, setTokenProvider } from './api';

afterEach(() => { vi.unstubAllGlobals(); setTokenProvider(null); });

it('identifies a missing route without claiming the server is stopped', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('404 page not found', { status: 404 })));
  await expect(request('/api/matches/')).rejects.toThrow('API route not found: GET /api/matches/ (404)');
});

it('preserves JSON resource errors from the API', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":"match not found"}', { status: 404 })));
  await expect(request('/api/matches/missing')).rejects.toThrow('match not found');
});
