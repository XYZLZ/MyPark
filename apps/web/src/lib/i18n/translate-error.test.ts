import { ApiError } from '@mypark/contracts';
import { describe, expect, test } from 'vitest';
import esDO from '@/i18n/messages/es-DO.json';
import { translateError, translateFieldMessage, type Translator } from './translate-error';

function lookup(key: string): string | undefined {
  let node: unknown = esDO;
  for (const part of key.split('.')) {
    if (typeof node !== 'object' || node === null) return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === 'string' ? node : undefined;
}

const t: Translator = Object.assign(
  (key: string, values: Record<string, string | number> = {}) =>
    (lookup(key) ?? `MISSING:${key}`).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? `{${name}}`)),
  { has: (key: string) => lookup(key) !== undefined },
);

const apiError = (code: string, details: Record<string, unknown> = {}) =>
  new ApiError({ code, message: 'secret server text', details });

describe('translateError', () => {
  test('translates a known code', () =>
    expect(translateError(t, apiError('vehicle.plate_taken'))).toBe('Esa placa ya está registrada.'));

  test('passes seconds for rate limits', () =>
    expect(translateError(t, apiError('rate_limit.exceeded', { retry_after_seconds: 42 }))).toBe(
      'Demasiados intentos. Inténtalo de nuevo en 42 s.',
    ));

  test('passes limit for vehicle.limit_reached', () =>
    expect(translateError(t, apiError('vehicle.limit_reached', { limit: 5 }))).toBe(
      'Alcanzaste el límite de 5 vehículos.',
    ));

  test('translates a client-only code', () =>
    expect(translateError(t, apiError('client.network'))).toContain('conectar'));

  test('unknown code uses errors.fallback, never message', () => {
    const text = translateError(t, apiError('made.up_code'));
    expect(text).toBe('Algo salió mal. Inténtalo de nuevo.');
    expect(text).not.toContain('secret server text');
  });
});

describe('translateFieldMessage', () => {
  test('handles rule, rule:param and errors: forms', () => {
    expect(translateFieldMessage(t, 'min:10')).toBe('Debe tener al menos 10 caracteres.');
    expect(translateFieldMessage(t, 'password_common')).toBe('Esa contraseña es muy común. Elige otra.');
    expect(translateFieldMessage(t, 'errors:vehicle.plate_taken')).toBe('Esa placa ya está registrada.');
  });

  test('an unlisted rule uses validation.unknown', () =>
    expect(translateFieldMessage(t, 'brand_new_rule:3')).toBe('Revisa este campo.'));

  test('an unknown errors: code uses errors.fallback', () =>
    expect(translateFieldMessage(t, 'errors:nope.nope')).toBe('Algo salió mal. Inténtalo de nuevo.'));
});
