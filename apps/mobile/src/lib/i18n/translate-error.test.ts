import { ApiError } from '@mypark/contracts';

import { initI18n } from './index';
import { translateError, translateFieldMessage } from './translate-error';

const es = initI18n('es-DO');
const en = initI18n('en');

const apiError = (code: string, details: Record<string, unknown> = {}) =>
  new ApiError({ code, message: 'secret server text', details });

test('uses errors.<code> and falls back to errors.fallback, never message', () => {
  expect(translateError(es, apiError('vehicle.plate_taken'))).toBe('Esa placa ya está registrada.');
  expect(translateError(es, apiError('client.network'))).toContain('conectar');
  const unknown = translateError(es, apiError('made.up_code'));
  expect(unknown).toBe('Algo salió mal. Inténtalo de nuevo.');
  expect(unknown).not.toContain('secret server text');
});

test('validation.min interpolates its param in both languages', () => {
  expect(translateFieldMessage(es, 'min:10')).toBe('Debe tener al menos 10 caracteres.');
  expect(translateFieldMessage(en, 'min:10')).toBe('Must be at least 10 characters.');
});

test('rate limit interpolates seconds', () => {
  expect(translateError(es, apiError('rate_limit.exceeded', { retry_after_seconds: 42 }))).toBe(
    'Demasiados intentos. Inténtalo de nuevo en 42 s.',
  );
  expect(translateError(en, apiError('rate_limit.exceeded', { retry_after_seconds: 42 }))).toContain('42');
});

test('limit_reached interpolates the limit', () => {
  expect(translateError(es, apiError('vehicle.limit_reached', { limit: 5 }))).toBe('Alcanzaste el límite de 5 vehículos.');
});

test('field messages handle rule, errors: and unknown rules', () => {
  expect(translateFieldMessage(es, 'password_common')).toBe('Esa contraseña es muy común. Elige otra.');
  expect(translateFieldMessage(es, 'errors:vehicle.plate_taken')).toBe('Esa placa ya está registrada.');
  expect(translateFieldMessage(es, 'brand_new_rule:3')).toBe('Revisa este campo.');
  expect(translateFieldMessage(es, 'errors:nope.nope')).toBe('Algo salió mal. Inténtalo de nuevo.');
});
