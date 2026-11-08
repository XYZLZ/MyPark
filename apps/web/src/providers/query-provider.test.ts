import { ApiError } from '@mypark/contracts';
import { expect, test } from 'vitest';
import { shouldRetry } from './query-provider';

const err = (code: string, status?: number) => new ApiError({ code, message: 'x', status });

test('retries a network failure and a 5xx, at most twice', () => {
  expect(shouldRetry(0, err('client.network'))).toBe(true);
  expect(shouldRetry(1, err('internal.unexpected', 503))).toBe(true);
  expect(shouldRetry(2, err('client.network'))).toBe(false);
});

test('never retries a 4xx or an unknown error', () => {
  expect(shouldRetry(0, err('vehicle.not_found', 404))).toBe(false);
  expect(shouldRetry(0, err('auth.unauthenticated', 401))).toBe(false);
  expect(shouldRetry(0, new Error('boom'))).toBe(false);
});
