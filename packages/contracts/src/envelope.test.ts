import { expect, test } from 'vitest';
import { z } from 'zod';
import { listBody, errorBody, successBody, vehicleFixture } from './fixtures/m1';
import { ApiError, listEnvelope, parseEnvelope, parseListEnvelope, parseNoContent } from './envelope';
import { vehicleSchema } from './m1/vehicle';

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

test('parseListEnvelope returns items and page', () => {
  expect(parseListEnvelope(vehicleSchema)(listBody([vehicleFixture], 5))).toEqual({
    items: [vehicleFixture],
    page: { limit: 5, next_cursor: null },
  });
});

test('parseListEnvelope throws the ApiError from an error envelope', () => {
  const err = thrown(() => parseListEnvelope(vehicleSchema)(errorBody('auth.unauthenticated'), 401));
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: 'auth.unauthenticated', status: 401 });
});

test('parseListEnvelope rejects a single-object body and a missing page', () => {
  expect(thrown(() => parseListEnvelope(vehicleSchema)(successBody(vehicleFixture)))).toMatchObject({ code: 'internal.unexpected' });
  const noPage = { data: [], meta: { request_id: 'r' } };
  expect(thrown(() => parseListEnvelope(vehicleSchema)(noPage))).toMatchObject({ code: 'internal.unexpected' });
});

test('parseNoContent accepts a 204 with no body', () => {
  expect(parseNoContent(undefined, 204)).toBeUndefined();
  expect(parseNoContent(null, 204)).toBeUndefined();
});

test('parseNoContent throws the envelope error on 409', () => {
  const err = thrown(() => parseNoContent(errorBody('vehicle.in_use'), 409));
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: 'vehicle.in_use', status: 409 });
});

test('parseNoContent treats a non-envelope error body as internal.unexpected', () => {
  expect(thrown(() => parseNoContent('<html>', 502))).toMatchObject({ code: 'internal.unexpected', status: 502 });
});

test('parseNoContent does not accept a 202 with a body', () => {
  const body = successBody({ status: 'accepted' });
  expect(thrown(() => parseNoContent(body, 202))).toMatchObject({ code: 'internal.unexpected', status: 202 });
});
