import { expect, expectTypeOf, test } from 'vitest';
import { ERROR_CODES } from './error-codes';
import type { components, paths } from './openapi';

type Schemas = components['schemas'];

const M2_CODES = [
  'device.unauthenticated',
  'device.timestamp_out_of_window',
  'device.event_not_allowed',
  'device.event_conflict',
  'lot.not_found',
  'lot.space_unknown',
  'gate.lot_full',
  'gate.lot_physically_full',
  'gate.lot_closed',
  'gate.credential_invalid',
  'gate.credential_in_use',
  'gate.payment_required',
  'gate.amount_mismatch',
  'gate.nothing_due',
  'gate.stay_unknown',
  'gate.exit_not_authorized',
  'gate.duplicate_pass',
  'simulator.unauthorized',
  'simulator.already_running',
  'simulator.not_running',
  'simulator.scenario_unavailable',
  'simulator.car_not_found',
  'session.logout_unconfirmed',
];

test('ERROR_CODES includes every M2 code', () => {
  expect(M2_CODES).toHaveLength(23);
  expect(ERROR_CODES).toEqual(expect.arrayContaining(M2_CODES));
  expect(ERROR_CODES).toHaveLength(48);
});

test('every M2 public lot path exists', () => {
  expectTypeOf<paths['/v1/lots']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/lots/{id}']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/lots/{id}/availability']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/lots/{id}/spaces']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/lots/{id}/tariff']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/lots/{id}/availability/stream']['get']>().not.toBeNever();
});

test('Availability has the design 11.1 fields', () => {
  expectTypeOf<Schemas['Availability']>().toHaveProperty('lot_id');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('available');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('occupied');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('effective_capacity');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('capacity');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('effective_holds');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('upcoming_reservations_2h');
  expectTypeOf<Schemas['Availability']['status']>().toEqualTypeOf<'open' | 'full' | 'closed'>();
  expectTypeOf<Schemas['Availability']>().toHaveProperty('version');
  expectTypeOf<Schemas['Availability']>().toHaveProperty('updated_at');
});

test('SpaceState and the SSE payloads have the design 11.3 shapes', () => {
  expectTypeOf<Schemas['SpaceState']['state']>().toEqualTypeOf<'free' | 'occupied' | 'out_of_service'>();
  expectTypeOf<Schemas['SpacesEvent']['full']>().toEqualTypeOf<boolean>();
  expectTypeOf<Schemas['SpacesEvent']['spaces']>().toEqualTypeOf<Schemas['SpaceState'][]>();
  expectTypeOf<Schemas['HeartbeatEvent']['as_of']>().toEqualTypeOf<string>();
});
