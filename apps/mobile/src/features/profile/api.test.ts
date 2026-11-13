import { successBody, userFixture } from '@mypark/contracts/fixtures';

import { ARGON2_TIMEOUT_MS } from '@/services/httpClient';
import { http } from '@/services/runtime';
import { reply } from '@/testing/response';

import { acceptLegal, deleteMe, getMe, updateMe } from './api';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

beforeEach(() => request.mockReset());

test('argon2 calls use the 20 s timeout', async () => {
  request.mockResolvedValue(reply(204));
  await deleteMe({ password: 'hunter2-correct-horse' });

  expect(ARGON2_TIMEOUT_MS).toBe(20_000);
  expect(request).toHaveBeenCalledWith({
    method: 'DELETE',
    path: '/v1/me',
    body: { password: 'hunter2-correct-horse' },
    auth: true,
    timeoutMs: 20_000,
  });
});

test('getMe and updateMe return the parsed user', async () => {
  request.mockResolvedValue(reply(200, successBody(userFixture)));
  await expect(getMe()).resolves.toEqual(userFixture);
  await expect(updateMe({ first_name: 'Ana' })).resolves.toEqual(userFixture);
  expect(request).toHaveBeenLastCalledWith({ method: 'PATCH', path: '/v1/me', body: { first_name: 'Ana' }, auth: true });
});

test('acceptLegal posts the versions and expects no content', async () => {
  request.mockResolvedValue(reply(204));
  await expect(acceptLegal({ terms_version: 'v1', privacy_version: 'v2' })).resolves.toBeUndefined();
  expect(request).toHaveBeenCalledWith({
    method: 'POST',
    path: '/v1/me/legal-acceptance',
    body: { terms_version: 'v1', privacy_version: 'v2' },
    auth: true,
  });
});
