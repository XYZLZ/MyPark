import type { components } from '../openapi';

type Schemas = components['schemas'];

/** Typed M3 fixtures for client tests and MSW handlers. Each `satisfies` its schema, so the contract cannot drift (no zod). */

const LOT_ID = '3f2b8c1e-6a4d-4e5f-9b7a-1c2d3e4f5a6b';
const RESERVATION_ID = '0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b';
const PAYMENT_ID = '0191a2b3-d6e7-7f80-9a1b-2c3d4e5f6a7b';

const vehicle = {
  id: '0191a2b3-0000-7000-8000-000000000001',
  plate_display: 'A123456',
  make: 'Toyota',
  model: 'Corolla',
  color: 'Gris',
} satisfies Schemas['ReservationVehicle'];

const holdWindow = {
  starts_at: '2026-12-01T14:45:00Z',
  ends_at: '2026-12-01T15:20:00Z',
} satisfies Schemas['HoldWindow'];

const base = {
  id: RESERVATION_ID,
  lot_id: LOT_ID,
  vehicle,
  arrival_at: '2026-12-01T15:00:00Z',
  hold_window: holdWindow,
  fee_cents: 15000,
  created_at: '2026-11-30T13:00:00Z',
  cancelled_at: null,
  cancelled_by: null,
  refund_reason: null,
  checked_in_at: null,
  completed_at: null,
  expired_at: null,
} as const;

export const reservationPendingFixture = {
  ...base,
  status: 'pending_payment',
  payment_due_at: '2026-11-30T13:15:00Z',
  confirmed_at: null,
  latest_payment: null,
  refund: null,
  stay: null,
} satisfies Schemas['Reservation'];

export const reservationConfirmedFixture = {
  ...base,
  status: 'confirmed',
  payment_due_at: '2026-11-30T13:15:00Z',
  confirmed_at: '2026-11-30T13:02:00Z',
  latest_payment: { id: PAYMENT_ID, status: 'succeeded', failure_code: null },
  refund: null,
  stay: null,
} satisfies Schemas['Reservation'];

export const reservationCheckedInFixture = {
  ...reservationConfirmedFixture,
  status: 'checked_in',
  checked_in_at: '2026-12-01T15:02:00Z',
  stay: { entered_at: '2026-12-01T15:02:00Z', exited_at: null, parking_paid_cents: 0 },
} satisfies Schemas['Reservation'];

const refund = {
  id: '0191a2b3-e8f9-7a0b-8c1d-3e4f5a6b7c8d',
  status: 'pending',
  amount_cents: 15000,
  reason: 'cancelled_before_arrival',
  succeeded_at: null,
} satisfies Schemas['ReservationRefund'];

export const reservationCancelledRefundPendingFixture = {
  ...reservationConfirmedFixture,
  status: 'cancelled',
  cancelled_at: '2026-11-30T20:00:00Z',
  cancelled_by: 'driver',
  refund_reason: 'cancelled_before_arrival',
  refund,
} satisfies Schemas['Reservation'];

export const quoteFixture = {
  lot_id: LOT_ID,
  arrival_at: '2026-12-01T15:00:00Z',
  hold_window: holdWindow,
  fee_cents: 15000,
  payment_timeout_min: 15,
} satisfies Schemas['Quote'];

/** Body of the `202` pay answer: the gateway did not answer in time. */
export const payPendingFixture = {
  reservation: reservationPendingFixture,
  payment: { id: PAYMENT_ID, status: 'pending' },
} satisfies Schemas['PayPending'];

/** Body of the `402` pay answer for the declining test card. */
export const payDeclinedErrorFixture = {
  error: {
    code: 'payment.declined',
    message: 'payment declined',
    details: { payment_id: PAYMENT_ID, failure_code: 'card_declined' },
  },
  meta: { request_id: '01HZX3V8Q2W9K7M4N5P6R7S8T9' },
} satisfies Schemas['ErrorEnvelope'];

export const reservationQrFixture = {
  token: 'fixture-token-DO-NOT-USE_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfGhIjKlMnOpQrStUvWxY',
  entry_valid_from: holdWindow.starts_at,
  entry_valid_until: holdWindow.ends_at,
} satisfies Schemas['ReservationQr'];

export const receiptFixture = {
  id: PAYMENT_ID,
  receipt_no: 'R-2026-000123',
  kind: 'reservation_fee',
  method: 'gateway_sim',
  amount_cents: 15000,
  status: 'succeeded',
  card_brand: 'visa',
  card_last4: '4242',
  failure_code: null,
  reservation_id: RESERVATION_ID,
  created_at: '2026-11-30T13:02:00Z',
  succeeded_at: '2026-11-30T13:02:01Z',
  refunded_at: null,
  refund: null,
} satisfies Schemas['Receipt'];
