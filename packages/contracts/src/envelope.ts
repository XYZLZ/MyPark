import { z } from 'zod';
import type { ErrorCode } from './error-codes';

/**
 * Zod mirrors of the baseline envelope schemas in apps/server/api/openapi.yaml.
 * Every OpenAPI object here declares `additionalProperties: false`, so the
 * zod schemas are strict. Drift against the generated types is caught at
 * compile time in envelope.types.test.ts.
 */

export const pageSchema = z.strictObject({
  limit: z.number().int(),
  next_cursor: z.string().nullable(),
});

export const metaSchema = z.strictObject({
  request_id: z.string(),
  page: pageSchema.optional(),
});

/** Error code pattern from the OpenAPI spec: `<domain>.<reason>` in snake_case. */
const errorCodePattern = /^[a-z][a-z0-9_]*[.][a-z][a-z0-9_]*$/;

export const errorBodySchema = z.strictObject({
  code: z.string().regex(errorCodePattern),
  message: z.string(),
  details: z.record(z.string(), z.unknown()),
});

export const errorEnvelopeSchema = z.strictObject({
  error: errorBodySchema,
  meta: metaSchema,
});

export function successEnvelope<S extends z.ZodType>(schema: S) {
  return z.strictObject({ data: schema, meta: metaSchema });
}

export function listEnvelope<S extends z.ZodType>(schema: S) {
  return z.strictObject({
    data: z.array(schema),
    meta: z.strictObject({ request_id: z.string(), page: pageSchema }),
  });
}

export type Page = z.infer<typeof pageSchema>;
export type Meta = z.infer<typeof metaSchema>;
export type ErrorEnvelope = z.infer<typeof errorEnvelopeSchema>;
export type ApiSuccess<T> = { data: T; meta: Meta };
export type ApiList<T> = { data: T[]; meta: Meta & { page: Page } };

/** Error built from a server error envelope, or from a body that is not an envelope. */
export class ApiError extends Error {
  readonly code: ErrorCode | string;
  readonly details: Record<string, unknown>;
  readonly requestId: string;
  readonly status?: number;

  constructor(init: {
    code: ErrorCode | string;
    message: string;
    details?: Record<string, unknown>;
    requestId?: string;
    status?: number;
  }) {
    super(init.message);
    this.name = 'ApiError';
    this.code = init.code;
    this.details = init.details ?? {};
    this.requestId = init.requestId ?? '';
    this.status = init.status;
  }
}

const MALFORMED_CODE = 'internal.unexpected' satisfies ErrorCode;

function malformed(status?: number): ApiError {
  return new ApiError({ code: MALFORMED_CODE, message: 'malformed envelope', status });
}

/** Throws the ApiError of an error envelope, or `internal.unexpected` for a body that is not an object. */
function rejectNonSuccess(body: unknown, status?: number): asserts body is object {
  if (typeof body !== 'object' || body === null || ('data' in body && 'error' in body)) {
    throw malformed(status);
  }
  if ('error' in body) {
    const parsed = errorEnvelopeSchema.safeParse(body);
    if (!parsed.success) throw malformed(status);
    const { error, meta } = parsed.data;
    throw new ApiError({
      code: error.code,
      message: error.message,
      details: error.details,
      requestId: meta.request_id,
      status,
    });
  }
}

/**
 * Returns a parser for a response body. It returns `data` when the body is a
 * success envelope whose `data` parses with `schema`, throws an ApiError built
 * from `error` when the body is an error envelope, and throws an ApiError with
 * code `internal.unexpected` (message `malformed envelope`) for anything else.
 */
export function parseEnvelope<S extends z.ZodType>(schema: S): (body: unknown, status?: number) => z.output<S> {
  const success = successEnvelope(schema);
  return (body, status) => {
    rejectNonSuccess(body, status);
    const parsed = success.safeParse(body);
    if (!parsed.success) throw malformed(status);
    return (parsed.data as { data: z.output<S> }).data;
  };
}

/** Like parseEnvelope for list responses: returns the items and `meta.page` (which `parseEnvelope` drops). */
export function parseListEnvelope<S extends z.ZodType>(
  schema: S,
): (body: unknown, status?: number) => { items: z.output<S>[]; page: Page } {
  const list = listEnvelope(schema);
  return (body, status) => {
    rejectNonSuccess(body, status);
    const parsed = list.safeParse(body);
    if (!parsed.success) throw malformed(status);
    const { data, meta } = parsed.data as { data: z.output<S>[]; meta: { page: Page } };
    return { items: data, page: meta.page };
  };
}

/**
 * For 204 responses only. Returns on a 204, throws the ApiError of an error
 * envelope, and throws `internal.unexpected` for anything else. A 202 carries a
 * body and must be parsed with parseEnvelope.
 */
export function parseNoContent(body: unknown, status: number): void {
  if (status === 204) return;
  rejectNonSuccess(body, status);
  throw malformed(status);
}
