import { ApiError } from '@mypark/contracts';
import { availabilityReducedFixture, errorBody, spacesFixture, successBody } from '@mypark/contracts/fixtures';
import { expect, test } from 'vitest';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { fetchLotSnapshot } from './api';

test('fetchLotSnapshot reads availability and spaces through the BFF in parallel', async () => {
  const fetchMock = stubFetch((url) =>
    url.endsWith('/availability')
      ? jsonResponse(200, successBody(availabilityReducedFixture))
      : jsonResponse(200, successBody(spacesFixture)),
  );
  await expect(fetchLotSnapshot('L 1')).resolves.toEqual({
    availability: availabilityReducedFixture,
    spaces: spacesFixture.spaces,
  });
  expect(fetchMock.mock.calls.map(([url]) => url).sort()).toEqual([
    '/api/v1/lots/L%201/availability',
    '/api/v1/lots/L%201/spaces',
  ]);
});

test('fetchLotSnapshot rejects with the API code', async () => {
  stubFetch(jsonResponse(404, errorBody('lot.not_found')));
  await expect(fetchLotSnapshot('L1')).rejects.toMatchObject({ code: 'lot.not_found' } satisfies Partial<ApiError>);
});
