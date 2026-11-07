import { z } from 'zod';
import { ApiError, parseEnvelope } from './envelope';
import type { ErrorCode } from './error-codes';

/**
 * Zod mirrors of the health schemas in apps/server/api/openapi.yaml.
 * Every OpenAPI object here declares `additionalProperties: false`, so the
 * zod schemas are strict. Drift against the generated types is caught at
 * compile time in health.types.test.ts. The envelope around them lives in
 * envelope.ts.
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

export type Health = z.infer<typeof healthSchema>;
export type ReadinessChecks = z.infer<typeof readinessChecksSchema>;
export type Readiness = z.infer<typeof readinessSchema>;

export type ApiHealthState = 'healthy' | 'not_ready' | 'unavailable';

export type HealthProbeResult = { status: number; body: unknown } | { networkError: true };

const NOT_READY_CODE = 'health.not_ready' satisfies ErrorCode;

const parseReadiness = parseEnvelope(readinessSchema);

/**
 * Maps a /readyz probe result to a UI health state.
 *
 * - 200 with a valid Readiness success envelope -> 'healthy'
 * - 503 with an error envelope whose code is health.not_ready -> 'not_ready'
 * - anything else (network error, malformed body, other status, the old M0
 *   error shape) -> 'unavailable'
 *
 * Never throws, whatever it is given at runtime.
 */
export function toHealthState(input: HealthProbeResult): ApiHealthState {
  try {
    if (typeof input !== 'object' || input === null || 'networkError' in input) {
      return 'unavailable';
    }
    if (input.status === 200) {
      parseReadiness(input.body, input.status);
      return 'healthy';
    }
    if (input.status === 503) {
      try {
        parseReadiness(input.body, input.status);
      } catch (err) {
        return err instanceof ApiError && err.code === NOT_READY_CODE ? 'not_ready' : 'unavailable';
      }
    }
    return 'unavailable';
  } catch {
    return 'unavailable';
  }
}
