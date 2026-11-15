export type { paths, components } from './openapi';
export { ERROR_CODES, type ErrorCode } from './error-codes';
export {
  ApiError,
  parseEnvelope,
  parseListEnvelope,
  parseNoContent,
  metaSchema,
  pageSchema,
  errorBodySchema,
  errorEnvelopeSchema,
  successEnvelope,
  listEnvelope,
  type Meta,
  type Page,
  type ApiSuccess,
  type ApiList,
  type ErrorEnvelope,
} from './envelope';
export {
  healthSchema,
  readinessChecksSchema,
  readinessSchema,
  toHealthState,
  type ApiHealthState,
  type HealthProbeResult,
  type Health,
  type ReadinessChecks,
  type Readiness,
} from './health';
export * from './m1/auth';
export * from './m1/legal';
export * from './m1/normalize';
export * from './m1/user';
export * from './m1/validation';
export * from './m1/vehicle';
export * from './m2/availability';
export * from './m2/lot';
export * from './m2/simulator';
export * from './m2/spaces';
export * from './m2/tariff';
