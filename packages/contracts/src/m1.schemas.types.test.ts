import { expectTypeOf, test } from 'vitest';
import type { z } from 'zod';
import type { components, paths } from './openapi';
import {
  acceptInvitationSchema,
  acceptLegalSchema,
  acceptedStatusSchema,
  adminUserSchema,
  deleteMeSchema,
  legalCurrentSchema,
  registerAcceptedSchema,
  resetSchema,
  tokenResponseSchema,
  toRegisterRequest,
  updateMeSchema,
  userSchema,
  vehicleInputSchema,
  vehicleSchema,
  verifySchema,
} from './index';

type Schemas = components['schemas'];

test('response schemas match the OpenAPI types', () => {
  expectTypeOf<z.output<typeof userSchema>>().toEqualTypeOf<Schemas['User']>();
  expectTypeOf<z.output<typeof tokenResponseSchema>>().toEqualTypeOf<Schemas['TokenResponse']>();
  expectTypeOf<z.output<typeof vehicleSchema>>().toEqualTypeOf<Schemas['Vehicle']>();
  expectTypeOf<z.output<typeof legalCurrentSchema>>().toEqualTypeOf<Schemas['LegalCurrent']>();
  expectTypeOf<z.output<typeof acceptedStatusSchema>>().toEqualTypeOf<Schemas['AcceptedStatus']>();
  expectTypeOf<z.output<typeof adminUserSchema>>().toEqualTypeOf<Schemas['AdminUser']>();
});

test('the register 202 body matches the OpenAPI response', () => {
  type RegisterBody = paths['/v1/auth/register']['post']['responses'][202]['content']['application/json']['data'];
  expectTypeOf<z.output<typeof registerAcceptedSchema>>().toEqualTypeOf<RegisterBody>();
});

test('request schemas match the OpenAPI requests', () => {
  expectTypeOf<z.output<typeof verifySchema>>().toEqualTypeOf<Schemas['VerifyEmailRequest']>();
  expectTypeOf<z.output<typeof resetSchema>>().toEqualTypeOf<Schemas['ResetPasswordRequest']>();
  expectTypeOf<z.output<typeof acceptInvitationSchema>>().toEqualTypeOf<Schemas['AcceptInvitationRequest']>();
  expectTypeOf<z.output<typeof vehicleInputSchema>>().toEqualTypeOf<Schemas['CreateVehicleRequest']>();
  expectTypeOf<z.output<typeof deleteMeSchema>>().toEqualTypeOf<Schemas['DeleteMeRequest']>();
  expectTypeOf<z.output<typeof acceptLegalSchema>>().toEqualTypeOf<Schemas['AcceptLegalRequest']>();
  expectTypeOf<ReturnType<typeof toRegisterRequest>>().toEqualTypeOf<Schemas['RegisterRequest']>();
  expectTypeOf<z.output<typeof updateMeSchema>>().toExtend<Schemas['UpdateMeRequest']>();
});
