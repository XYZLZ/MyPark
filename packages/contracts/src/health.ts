import { z } from 'zod';
import type { ErrorCode } from './error-codes';

/**
 * Zod mirrors of the health schemas in apps/server/api/openapi.yaml.
 * Every OpenAPI object here declares `additionalProperties: false`, so the
 * zod schemas are strict. Drift against the generated types is caught at
 * compile time in health.types.test.ts.
 */

export const healthSchema = z.strictObject({
  status: z.literal('ok'),
  service: z.string(),
  version: z.string(),
});

export const readinessChecksSchema = z.strictObject({
  database: z.enum(['ok', 'fail']),
  migrations: z.enum(['ok', 'behind', 'fail']),
});

export const readinessSchema = z.strictObject({
  status: z.literal('ready'),
  checks: readinessChecksSchema,
});

/** Error code pattern from the OpenAPI spec: `<domain>.<reason>` in snake_case. */
const errorCodePattern = /^[a-z][a-z0-9_]*[.][a-z][a-z0-9_]*$/;

export const errorEnvelopeSchema = z.strictObject({
  error: z.strictObject({
    code: z.string().regex(errorCodePattern),
    message: z.string(),
    details: z.record(z.string(), z.unknown()).optional(),
    request_id: z.string(),
  }),
});

export type Health = z.infer<typeof healthSchema>;
export type ReadinessChecks = z.infer<typeof readinessChecksSchema>;
export type Readiness = z.infer<typeof readinessSchema>;
export type ErrorEnvelope = z.infer<typeof errorEnvelopeSchema>;

export type ApiHealthState = 'healthy' | 'not_ready' | 'unavailable';

export type HealthProbeResult = { status: number; body: unknown } | { networkError: true };

const NOT_READY_CODE = 'health.not_ready' satisfies ErrorCode;

/**
 * Maps a /readyz probe result to a UI health state.
 *
 * - 200 with a valid Readiness body -> 'healthy'
 * - 503 with a valid ErrorEnvelope whose code is health.not_ready -> 'not_ready'
 * - anything else (network error, malformed body, other status) -> 'unavailable'
 *
 * Never throws, whatever it is given at runtime.
 */
export function toHealthState(input: HealthProbeResult): ApiHealthState {
  try {
    if (typeof input !== 'object' || input === null || 'networkError' in input) {
      return 'unavailable';
    }
    if (input.status === 200) {
      return readinessSchema.safeParse(input.body).success ? 'healthy' : 'unavailable';
    }
    if (input.status === 503) {
      const parsed = errorEnvelopeSchema.safeParse(input.body);
      return parsed.success && parsed.data.error.code === NOT_READY_CODE ? 'not_ready' : 'unavailable';
    }
    return 'unavailable';
  } catch {
    return 'unavailable';
  }
}
