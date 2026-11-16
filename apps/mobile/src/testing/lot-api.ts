import type { Availability } from '@mypark/contracts';
import {
  availabilityReducedFixture,
  lotFixture,
  lotSummaryFixture,
  listBody,
  spacesFixture,
  successBody,
  tariffFixture,
} from '@mypark/contracts/fixtures';

import type { http } from '@/services/runtime';

import { reply } from './response';

/** Answers the five public lot GETs from the shared fixtures. `http.request` must be a jest mock. */
export function stubLotApi(request: jest.MockedFunction<typeof http.request>, availability: Availability = availabilityReducedFixture): void {
  request.mockImplementation(async ({ path }) => {
    if (path === '/v1/lots') return reply(200, listBody([lotSummaryFixture], 20));
    if (path.endsWith('/availability')) return reply(200, successBody(availability));
    if (path.endsWith('/spaces')) return reply(200, successBody(spacesFixture));
    if (path.endsWith('/tariff')) return reply(200, successBody(tariffFixture));
    return reply(200, successBody(lotFixture));
  });
}

/** The requests made for one lot resource, e.g. `requestsFor(request, '/availability')`. */
export function requestsFor(request: jest.MockedFunction<typeof http.request>, suffix: string): number {
  return request.mock.calls.filter(([{ path }]) => path.endsWith(suffix)).length;
}
