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

test('postDeviceEvent exists and requires the three HMAC headers', () => {
  expectTypeOf<paths['/v1/devices/events']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/devices/events']['post']['parameters']['header']>().toEqualTypeOf<{
    'X-Device-Id': string;
    'X-Timestamp': string;
    'X-Signature': string;
  }>();
});

test('DeviceEvent is a union discriminated by type with nine variants', () => {
  expectTypeOf<Schemas['DeviceEvent']['type']>().toEqualTypeOf<
    | 'podium.vehicle_detected'
    | 'entry.credential_presented'
    | 'barrier.passed'
    | 'paystation.scan'
    | 'paystation.lost_ticket'
    | 'paystation.cash_paid'
    | 'exit.credential_presented'
    | 'sensor.space_changed'
    | 'heartbeat'
  >();
  expectTypeOf<Schemas['DeviceEventHeartbeat']['type']>().toEqualTypeOf<'heartbeat'>();
  expectTypeOf<Schemas['DeviceEventPaystationCashPaid']['payload']['amount_cents']>().toEqualTypeOf<number>();
});

test('DeviceEventResult.result is ack|open|deny|rejected', () => {
  expectTypeOf<Schemas['DeviceEventResult']['result']>().toEqualTypeOf<'ack' | 'open' | 'deny' | 'rejected'>();
  expectTypeOf<Schemas['DeviceEventResult']['breakdown']>().toEqualTypeOf<Schemas['FeeBreakdown'] | undefined>();
});

test('every simulator control path exists', () => {
  expectTypeOf<paths['/control/status']['get']>().not.toBeNever();
  expectTypeOf<paths['/control/scenarios/start']['post']>().not.toBeNever();
  expectTypeOf<paths['/control/scenarios/stop']['post']>().not.toBeNever();
  expectTypeOf<paths['/control/cars']['post']>().not.toBeNever();
  expectTypeOf<paths['/control/cars/{car_id}/leave']['post']>().not.toBeNever();
  expectTypeOf<paths['/control/sensors/{code}/fault']['post']>().not.toBeNever();
  expectTypeOf<paths['/control/events']['get']>().not.toBeNever();
});

test('StartScenarioRequest.scenario lists the seven scenario names', () => {
  expectTypeOf<Schemas['StartScenarioRequest']['scenario']>().toEqualTypeOf<
    | 'idle'
    | 'weekday'
    | 'weekend_peak'
    | 'fill_to_full'
    | 'sensor_drift'
    | 'reservation_arrivals'
    | 'no_show'
  >();
});

test('InjectCarRequest and SensorFaultRequest use the design 13.3 enums', () => {
  expectTypeOf<Schemas['InjectCarRequest']['credential']>().toEqualTypeOf<'ticket' | 'employee_card' | 'reservation_qr'>();
  expectTypeOf<Schemas['SensorFaultRequest']['fault']>().toEqualTypeOf<
    'stuck_occupied' | 'stuck_free' | 'silent' | 'none'
  >();
  expectTypeOf<Schemas['InjectCarResponse']['car_id']>().toEqualTypeOf<string>();
});
