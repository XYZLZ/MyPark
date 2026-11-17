import { expect, expectTypeOf, test } from 'vitest';
import { ERROR_CODES } from './error-codes';
import {
  payDeclinedErrorFixture,
  payPendingFixture,
  quoteFixture,
  receiptFixture,
  reservationCancelledRefundPendingFixture,
  reservationCheckedInFixture,
  reservationConfirmedFixture,
  reservationPendingFixture,
  reservationQrFixture,
} from './fixtures/m3';
import type { components, paths } from './openapi';

type Schemas = components['schemas'];

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

test('every M3 driver path exists', () => {
  expectTypeOf<paths['/v1/reservations/quote']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations/{id}']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations/{id}']['patch']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations/{id}/pay']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations/{id}/qr']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/reservations/{id}/cancel']['post']>().not.toBeNever();
  expectTypeOf<paths['/v1/payments']['get']>().not.toBeNever();
  expectTypeOf<paths['/v1/payments/{id}']['get']>().not.toBeNever();
});

test('pay requires the Idempotency-Key header', () => {
  expectTypeOf<paths['/v1/reservations/{id}/pay']['post']['parameters']['header']>().toEqualTypeOf<{
    'Idempotency-Key': string;
  }>();
  expectTypeOf<Schemas['PayRequest']>().toHaveProperty('card_number');
  expectTypeOf<Schemas['PayRequest']>().toHaveProperty('exp_month');
  expectTypeOf<Schemas['PayRequest']>().toHaveProperty('exp_year');
  expectTypeOf<Schemas['PayRequest']>().toHaveProperty('cvc');
});

test('Reservation.status has the seven states', () => {
  expectTypeOf<Schemas['Reservation']['status']>().toEqualTypeOf<
    | 'pending_payment'
    | 'confirmed'
    | 'checked_in'
    | 'completed'
    | 'cancelled'
    | 'expired_unpaid'
    | 'expired_no_show'
  >();
});

test('InjectCarRequest.credential accepts reservation_qr', () => {
  expectTypeOf<'reservation_qr'>().toExtend<Schemas['InjectCarRequest']['credential']>();
  expectTypeOf<Schemas['InjectCarRequest']>().toHaveProperty('qr');
});

test('fixtures parse as their schema types', () => {
  expect(reservationPendingFixture.status).toBe('pending_payment');
  expect(reservationConfirmedFixture.status).toBe('confirmed');
  expect(reservationCheckedInFixture.stay).not.toBeNull();
  expect(reservationCancelledRefundPendingFixture.refund?.status).toBe('pending');
  expect(quoteFixture.fee_cents).toBeGreaterThan(0);
  expect(payPendingFixture.payment.status).toBe('pending');
  expect(payDeclinedErrorFixture.error.code).toBe('payment.declined');
  expect(reservationQrFixture.token).toMatch(/^[A-Za-z0-9_-]{86}$/);
  expect(receiptFixture.receipt_no).not.toBeNull();
  const w = reservationConfirmedFixture.hold_window;
  expect(Date.parse(w.ends_at)).toBeGreaterThan(Date.parse(w.starts_at));
});
