import { describe, expect, it } from 'vitest';
import { parseRoute } from './router';

describe('parseRoute', () => {
  it('parses static and parameter routes', () => {
    expect(parseRoute('#/')).toMatchObject({ name: 'home' });
    expect(parseRoute('#/match/m-1')).toMatchObject({ name: 'match', parameter: 'm-1' });
    expect(parseRoute('#/match/m-1/detail')).toMatchObject({ name: 'match-detail', parameter: 'm-1' });
    expect(parseRoute('#/profile/Ada%20L')).toMatchObject({ name: 'profile', parameter: 'Ada L' });
  });
});
