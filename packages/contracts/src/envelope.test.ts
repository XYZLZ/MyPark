import { expect, test } from 'vitest';
import { z } from 'zod';
import { ApiError, listEnvelope, parseEnvelope } from './envelope';

const schema = z.object({ a: z.number() });

function thrown(fn: () => unknown): unknown {
  try {
    fn();
  } catch (e) {
    return e;
  }
  throw new Error('expected a throw');
}

test('parseEnvelope returns data for a success envelope', () => {
  expect(parseEnvelope(schema)({ data: { a: 1 }, meta: { request_id: 'r' } })).toEqual({ a: 1 });
});

test('parseEnvelope throws ApiError with code, details, requestId', () => {
  const body = { error: { code: 'auth.forbidden', message: 'x', details: { k: 1 } }, meta: { request_id: 'r' } };
  const err = thrown(() => parseEnvelope(schema)(body, 403));
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: 'auth.forbidden', details: { k: 1 }, requestId: 'r', status: 403, message: 'x' });
});

test('parseEnvelope rejects a body with both data and error', () => {
  const body = {
    data: { a: 1 },
    error: { code: 'auth.forbidden', message: 'x', details: {} },
    meta: { request_id: 'r' },
  };
  expect(thrown(() => parseEnvelope(schema)(body))).toMatchObject({ code: 'internal.unexpected' });
});

test('parseEnvelope rejects data that fails the schema', () => {
  const err = thrown(() => parseEnvelope(schema)({ data: { a: 'no' }, meta: { request_id: 'r' } }));
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: 'internal.unexpected', message: 'malformed envelope' });
});

test('parseEnvelope rejects non-object bodies and the old error shape', () => {
  for (const body of [null, undefined, 'x', 3]) {
    expect(thrown(() => parseEnvelope(schema)(body))).toMatchObject({ code: 'internal.unexpected' });
  }
  const oldShape = { error: { code: 'auth.forbidden', message: 'x', request_id: 'r' } };
  expect(thrown(() => parseEnvelope(schema)(oldShape))).toMatchObject({ message: 'malformed envelope' });
});

test('listEnvelope requires meta.page with nullable next_cursor', () => {
  const list = listEnvelope(schema);
  const meta = { request_id: 'r', page: { limit: 5, next_cursor: null } };
  expect(list.safeParse({ data: [{ a: 1 }], meta }).success).toBe(true);
  expect(list.safeParse({ data: [], meta: { ...meta, page: { limit: 5, next_cursor: 'c' } } }).success).toBe(true);
  expect(list.safeParse({ data: [], meta: { request_id: 'r' } }).success).toBe(false);
  expect(list.safeParse({ data: [], meta: { request_id: 'r', page: { limit: 5 } } }).success).toBe(false);
});
