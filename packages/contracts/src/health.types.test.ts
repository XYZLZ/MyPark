import { expectTypeOf, test } from 'vitest';
import type { ErrorEnvelope, Health, Readiness, ReadinessChecks } from './health';
import type { components } from './openapi';

type Schemas = components['schemas'];

// Both directions: zod -> OpenAPI catches a zod field the spec lacks or a
// wider zod type; OpenAPI -> zod catches a spec field the zod schema forgot.
test('zod Health matches the OpenAPI Health schema', () => {
  expectTypeOf<Health>().toEqualTypeOf<Schemas['Health']>();
});

test('zod ReadinessChecks matches the OpenAPI ReadinessChecks schema', () => {
  expectTypeOf<ReadinessChecks>().toEqualTypeOf<Schemas['ReadinessChecks']>();
});

test('zod Readiness matches the OpenAPI Readiness schema', () => {
  expectTypeOf<Readiness>().toEqualTypeOf<Schemas['Readiness']>();
});

test('zod ErrorEnvelope matches the OpenAPI ErrorEnvelope schema', () => {
  expectTypeOf<ErrorEnvelope>().toExtend<Schemas['ErrorEnvelope']>();
  expectTypeOf<Schemas['ErrorEnvelope']>().toExtend<ErrorEnvelope>();
});
