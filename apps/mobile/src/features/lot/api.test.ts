import { ApiError } from '@mypark/contracts';
import {
  errorBody,
  listBody,
  lotFixture,
  lotSummaryFixture,
  spacesFixture,
  successBody,
  tariffFixture,
} from '@mypark/contracts/fixtures';

import { http } from '@/services/runtime';
import { reply } from '@/testing/response';

import { getAvailability, getLot, getLots, getSpaces, getTariff } from './api';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);
const ID = lotSummaryFixture.id;

beforeEach(() => request.mockReset());

test('getLots returns the items', async () => {
  request.mockResolvedValue(reply(200, listBody([lotSummaryFixture], 20)));
  await expect(getLots()).resolves.toEqual([lotSummaryFixture]);
  // Public: no `auth`, so the client sends no bearer.
  expect(request).toHaveBeenCalledWith({ method: 'GET', path: '/v1/lots' });
});

test('getLot, getSpaces and getTariff parse their envelopes from encoded paths', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(lotFixture)));
  await expect(getLot(ID)).resolves.toEqual(lotFixture);
  request.mockResolvedValueOnce(reply(200, successBody(spacesFixture)));
  await expect(getSpaces('a/b')).resolves.toEqual(spacesFixture.spaces);
  expect(request).toHaveBeenLastCalledWith({ method: 'GET', path: '/v1/lots/a%2Fb/spaces' });
  request.mockResolvedValueOnce(reply(200, successBody(tariffFixture)));
  await expect(getTariff(ID)).resolves.toEqual(tariffFixture);
  expect(request).toHaveBeenLastCalledWith({ method: 'GET', path: `/v1/lots/${ID}/tariff` });
});

test('getAvailability surfaces lot.not_found as an ApiError', async () => {
  request.mockResolvedValue(reply(404, errorBody('lot.not_found')));
  const error = await getAvailability(ID).catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  expect(error).toMatchObject({ code: 'lot.not_found' });
  expect(request).toHaveBeenCalledWith({ method: 'GET', path: `/v1/lots/${ID}/availability` });
});
