// @vitest-environment node
import {
  availabilityReducedFixture,
  errorBody,
  listBody,
  lotFixture,
  lotSummaryFixture,
  spacesFixture,
  successBody,
  tariffFixture,
} from '@mypark/contracts/fixtures';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { jsonResponse, stubFetch } from '@/testing/fetch';

vi.mock('next/headers', () => ({
  cookies: async () => ({ get: () => undefined }),
  headers: async () => new Headers(),
}));

beforeEach(() => {
  vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
});

const LOT = lotSummaryFixture.id;

/** The API at the fetch boundary; `override` replaces one path's response. */
function stubApi(override: Record<string, Response> = {}) {
  const routes: Record<string, () => Response> = {
    '/v1/lots': () => jsonResponse(200, listBody([lotSummaryFixture], 50)),
    [`/v1/lots/${LOT}`]: () => jsonResponse(200, successBody(lotFixture)),
    [`/v1/lots/${LOT}/availability`]: () => jsonResponse(200, successBody(availabilityReducedFixture)),
    [`/v1/lots/${LOT}/spaces`]: () => jsonResponse(200, successBody(spacesFixture)),
    [`/v1/lots/${LOT}/tariff`]: () => jsonResponse(200, successBody(tariffFixture)),
  };
  return stubFetch((url) => {
    const path = new URL(url).pathname;
    return override[path]?.clone() ?? routes[path]!();
  });
}

describe('loadLotSnapshot', () => {
  test('loadLotSnapshot uses the first lot and loads four resources', async () => {
    const fetchMock = stubApi();
    const { loadLotSnapshot } = await import('./server');

    const snapshot = await loadLotSnapshot(() => 1_700_000_000_000);

    expect(snapshot).toEqual({
      lot: lotFixture,
      availability: availabilityReducedFixture,
      spaces: spacesFixture.spaces,
      tariff: tariffFixture,
      fetchedAt: 1_700_000_000_000,
    });
    expect(fetchMock).toHaveBeenCalledTimes(5);
    expect(fetchMock.mock.calls.map(([url]) => new URL(url).pathname).sort()).toEqual(
      [
        '/v1/lots',
        `/v1/lots/${LOT}`,
        `/v1/lots/${LOT}/availability`,
        `/v1/lots/${LOT}/spaces`,
        `/v1/lots/${LOT}/tariff`,
      ].sort(),
    );
    // Public reads: no bearer, and the 5 s timeout of the other server reads.
    for (const [, init] of fetchMock.mock.calls) {
      expect(new Headers(init?.headers).has('authorization')).toBe(false);
      expect(init?.signal).toBeInstanceOf(AbortSignal);
    }
  });

  test('loadLotSnapshot returns null when the list is empty', async () => {
    const fetchMock = stubApi({ '/v1/lots': jsonResponse(200, listBody([], 50)) });
    const { loadLotSnapshot } = await import('./server');

    await expect(loadLotSnapshot()).resolves.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test.each(['', '/availability', '/spaces', '/tariff'])('loadLotSnapshot returns null when %s fails', async (suffix) => {
    stubApi({ [`/v1/lots/${LOT}${suffix}`]: jsonResponse(500, errorBody('internal.unexpected')) });
    const { loadLotSnapshot } = await import('./server');

    await expect(loadLotSnapshot()).resolves.toBeNull();
  });

  test('loadLotSnapshot returns null when the list call fails', async () => {
    stubApi({ '/v1/lots': jsonResponse(503, errorBody('internal.unexpected')) });
    const { loadLotSnapshot } = await import('./server');

    await expect(loadLotSnapshot()).resolves.toBeNull();
  });
});
