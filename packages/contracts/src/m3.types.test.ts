import { expect, test } from 'vitest';
import { ERROR_CODES } from './error-codes';

const M3_CODES = [
  'reservation.not_found',
  'reservation.capacity_reached',
  'reservation.active_exists',
  'reservation.blocked_no_shows',
  'reservation.lot_closed',
  'reservation.no_space_now',
  'reservation.arrival_in_past',
  'reservation.beyond_horizon',
  'reservation.invalid_state',
  'reservation.payment_expired',
  'reservation.vehicle_change_closed',
  'reservation.too_early',
  'reservation.window_closed',
  'payment.declined',
  'payment.in_progress',
  'payment.not_found',
  'idempotency.key_reused',
];

test('ERROR_CODES includes every M3 code', () => {
  expect(M3_CODES).toHaveLength(17);
  expect(ERROR_CODES).toEqual(expect.arrayContaining(M3_CODES));
  expect(ERROR_CODES).toHaveLength(65);
});
