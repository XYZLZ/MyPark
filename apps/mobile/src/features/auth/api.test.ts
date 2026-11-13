import { legalCurrentFixture, successBody, tokenResponseFixture } from '@mypark/contracts/fixtures';

import { http } from '@/services/runtime';
import { reply } from '@/testing/response';

import { fetchLegal, forgotPassword, login, register, resendVerification, resetPassword, verify } from './api';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

beforeEach(() => request.mockReset());

test('login sends client_kind mobile', async () => {
  request.mockResolvedValue(reply(200, successBody(tokenResponseFixture)));

  await expect(login({ email: 'ana@example.do', password: 'correct horse' })).resolves.toEqual(tokenResponseFixture);

  expect(request).toHaveBeenCalledWith(
    expect.objectContaining({
      method: 'POST',
      path: '/v1/auth/login',
      body: { email: 'ana@example.do', password: 'correct horse', client_kind: 'mobile' },
    }),
  );
});

test('argon2 calls use the 20 s timeout', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(tokenResponseFixture)));
  request.mockResolvedValueOnce(reply(202, successBody({ status: 'verification_pending' })));
  request.mockResolvedValueOnce(reply(204));

  await login({ email: 'a@b.co', password: 'x' });
  await register({
    email: 'a@b.co',
    password: 'correct horse battery',
    first_name: 'Ana',
    last_name: 'Perez',
    locale: 'en',
    accepted_terms_version: 'v1',
    accepted_privacy_version: 'v1',
  });
  await resetPassword({ email: 'a@b.co', code: '123456', new_password: 'correct horse battery' });

  expect(request.mock.calls.map(([req]) => [req.path, req.timeoutMs])).toEqual([
    ['/v1/auth/login', 20000],
    ['/v1/auth/register', 20000],
    ['/v1/auth/password/reset', 20000],
  ]);
});

test('the other calls use the default timeout and the documented paths', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(legalCurrentFixture)));
  request.mockResolvedValueOnce(reply(204));
  request.mockResolvedValue(reply(202, successBody({ status: 'accepted' })));

  await expect(fetchLegal()).resolves.toEqual(legalCurrentFixture);
  await verify({ email: 'a@b.co', code: '123456' });
  await resendVerification('a@b.co');
  await forgotPassword('a@b.co');

  expect(request.mock.calls.map(([req]) => [req.method, req.path, req.timeoutMs, req.auth])).toEqual([
    ['GET', '/v1/legal/current', undefined, undefined],
    ['POST', '/v1/auth/verify', undefined, undefined],
    ['POST', '/v1/auth/verify/resend', undefined, undefined],
    ['POST', '/v1/auth/password/forgot', undefined, undefined],
  ]);
});

test('an error envelope becomes an ApiError that keeps Retry-After', async () => {
  request.mockResolvedValue(
    reply(429, { error: { code: 'rate_limit.exceeded', message: 'x', details: {} }, meta: { request_id: 'r' } }, '30'),
  );

  await expect(login({ email: 'a@b.co', password: 'x' })).rejects.toMatchObject({
    code: 'rate_limit.exceeded',
    details: { retry_after_header: '30' },
  });
});
