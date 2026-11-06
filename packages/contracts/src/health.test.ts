import { expect, test } from 'vitest';
import { ERROR_CODES } from './error-codes';
import { toHealthState } from './health';

test('toHealthState maps 200 ready to healthy', () =>
  expect(toHealthState({ status: 200, body: { status: 'ready', checks: { database: 'ok', migrations: 'ok' } } })).toBe('healthy'));
test('toHealthState maps 503 envelope to not_ready', () =>
  expect(toHealthState({ status: 503, body: { error: { code: 'health.not_ready', message: 'x', request_id: 'r' } } })).toBe('not_ready'));
test('toHealthState maps network error to unavailable', () =>
  expect(toHealthState({ networkError: true })).toBe('unavailable'));
test('toHealthState maps malformed 200 body to unavailable', () =>
  expect(toHealthState({ status: 200, body: { foo: 1 } })).toBe('unavailable'));
test('ERROR_CODES contains health.not_ready', () => expect(ERROR_CODES).toContain('health.not_ready'));

test('toHealthState maps 500 to unavailable', () =>
  expect(toHealthState({ status: 500, body: { error: { code: 'internal.unexpected', message: 'x', request_id: 'r' } } })).toBe('unavailable'));
test('toHealthState maps 503 with a different code to unavailable', () =>
  expect(toHealthState({ status: 503, body: { error: { code: 'internal.unexpected', message: 'x', request_id: 'r' } } })).toBe('unavailable'));
test('toHealthState maps 503 envelope with details to not_ready', () =>
  expect(
    toHealthState({
      status: 503,
      body: { error: { code: 'health.not_ready', message: 'x', details: { database: 'ok', migrations: 'behind' }, request_id: 'r' } },
    }),
  ).toBe('not_ready'));
test('toHealthState maps 200 with a not-ready-looking body to unavailable', () =>
  expect(toHealthState({ status: 200, body: { status: 'ready', checks: { database: 'down', migrations: 'ok' } } })).toBe('unavailable'));
test('toHealthState never throws on garbage input', () => {
  for (const input of [null, undefined, 'x', 42, [], {}, { status: '200', body: null }] as never[]) {
    expect(toHealthState(input)).toBe('unavailable');
  }
});
