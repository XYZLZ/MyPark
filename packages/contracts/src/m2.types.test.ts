import { expect, test } from 'vitest';
import { ERROR_CODES } from './error-codes';

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
