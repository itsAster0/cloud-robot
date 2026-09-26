import { describe, expect, it } from 'vitest';
import { parseRoute, routeHref } from './router';

describe('parseRoute', () => {
  it('parses static and parameter routes', () => {
    expect(parseRoute('#/')).toMatchObject({ name: 'home' });
    expect(parseRoute('#/match/m-1')).toMatchObject({ name: 'match', parameter: 'm-1' });
    expect(parseRoute('#/match/m-1/detail')).toMatchObject({ name: 'match-detail', parameter: 'm-1' });
    expect(parseRoute('#/profile/Ada%20L')).toMatchObject({ name: 'profile', parameter: 'Ada L' });
  });

  it.each([
    ['/workspace', 'code'], ['/workspace/code', 'code'], ['/workspace/build', 'build'],
    ['/workspace/matches', 'match'], ['/workspace/match', 'match'],
    ['/workspace/test', 'debug'], ['/workspace/debug', 'debug'], ['/workspace/results', 'results'],
  ])('opens %s in the matching workspace stage', (path, panel) => {
    expect(parseRoute(routeHref(path))).toEqual({ name: 'workspace', path, panel });
    expect(parseRoute(routeHref(`${path}/`))).toEqual({ name: 'workspace', path, panel });
  });

  it('keeps arena invites and public history separate from workspace stages', () => {
    expect(parseRoute('#/v2')).toMatchObject({ name: 'v2', parameter: undefined });
    expect(parseRoute('#/v2/match-42')).toMatchObject({ name: 'v2', parameter: 'match-42' });
    expect(parseRoute('#/matches')).toMatchObject({ name: 'matches' });
    expect(parseRoute('#/spectate')).toMatchObject({ name: 'spectate' });
    expect(parseRoute('#/box')).toMatchObject({ name: 'box' });
    expect(parseRoute('#/play')).toMatchObject({ name: 'play' });
  });

  it('returns a missing route for unknown stages and malformed copied links', () => {
    expect(parseRoute('#/workspace/unknown')).toMatchObject({ name: 'not-found' });
    expect(parseRoute('#/profile/%invalid')).toMatchObject({ name: 'not-found' });
  });
});
