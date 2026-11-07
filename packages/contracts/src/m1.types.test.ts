import { expect, expectTypeOf, test } from 'vitest';
import { ERROR_CODES } from './error-codes';
import type { components, paths } from './openapi';

type Schemas = components['schemas'];

test('every M1 path and method exists', () => {
  expectTypeOf<paths['/v1/legal/current']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/register']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/verify']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/verify/resend']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/login']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/refresh']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/logout']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/logout-all']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/password/forgot']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/password/reset']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/auth/invitations/accept']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/me']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/me']['patch']>().not.toBeNever();
  expectTypeOf<paths['/v1/me']['delete']>().not.toBeNever();
  expectTypeOf<paths['/v1/me/legal-acceptance']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/vehicles']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/vehicles']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/vehicles/{id}']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/vehicles/{id}']['patch']>().not.toBeNever();
  expectTypeOf<paths['/v1/vehicles/{id}']['delete']>().not.toBeNever();
  expectTypeOf<paths['/v1/admin/admins']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/admin/admins']['post']>().not.toBeNever();
});

test('ERROR_CODES includes every M1 code', () => {
  expect(ERROR_CODES).toEqual(
    expect.arrayContaining([
      'auth.unauthenticated',
      'auth.session_revoked',
      'auth.refresh_invalid',
      'auth.invalid_credentials',
      'auth.email_not_verified',
      'auth.account_suspended',
      'auth.forbidden',
      'auth.password_incorrect',
      'auth.code_invalid',
      'legal.acceptance_required',
      'legal.version_outdated',
      'user.email_taken',
      'vehicle.not_found',
      'vehicle.plate_taken',
      'vehicle.limit_reached',
      'vehicle.in_use',
      'rate_limit.exceeded',
      'http.invalid_json',
      'http.payload_too_large',
      'http.unsupported_media_type',
    ]),
  );
});

test('TokenResponse has first_name and last_name on user, never full_name', () => {
  type TokenUser = Schemas['TokenResponse']['user'];
  expectTypeOf<TokenUser['first_name']>().toBeString();
  expectTypeOf<TokenUser['last_name']>().toBeString();
  expectTypeOf<keyof TokenUser>().not.toExtend<'full_name'>();
  expectTypeOf<'full_name'>().not.toExtend<keyof TokenUser>();
  expectTypeOf<Schemas['AdminUser']['first_name']>().toBeString();
  expectTypeOf<Schemas['AdminUser']['last_name']>().toBeString();
});

test('list responses always carry meta.page (required, non-optional)', () => {
  type ListBody<T> = T extends { content: { 'application/json': infer B } } ? B : never;
  type Vehicles = ListBody<paths['/v1/vehicles']['get']['responses']['200']>;
  type Admins = ListBody<paths['/v1/admin/admins']['get']['responses']['200']>;
  expectTypeOf<Vehicles['meta']['page']>().toEqualTypeOf<Schemas['Page']>();
  expectTypeOf<Admins['meta']['page']>().toEqualTypeOf<Schemas['Page']>();
});
