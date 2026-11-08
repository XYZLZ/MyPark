import { ApiError } from '@mypark/contracts';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import { translateError, translateFieldMessage, type Translator } from './translate-error';

function makeTranslator(messages: unknown): Translator {
  const lookup = (key: string): string | undefined => {
    let node: unknown = messages;
    for (const part of key.split('.')) {
      if (typeof node !== 'object' || node === null) return undefined;
      node = (node as Record<string, unknown>)[part];
    }
    return typeof node === 'string' ? node : undefined;
  };
  return Object.assign(
    (key: string, values: Record<string, string | number> = {}) =>
      (lookup(key) ?? `MISSING:${key}`).replace(/\{(\w+)\}/g, (_, name: string) => {
        // A placeholder with no value would reach the user as a raw token, so fail loudly.
        if (!(name in values)) throw new Error(`${key}: no value passed for {${name}}`);
        return String(values[name]);
      }),
    { has: (key: string) => lookup(key) !== undefined },
  );
}

const t = makeTranslator(esDO);
const tEn = makeTranslator(en);

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

describe('english interpolation', () => {
  test('min fills {param}', () => expect(translateFieldMessage(tEn, 'min:10')).toBe('Must be at least 10 characters.'));

  test('limit_reached fills {limit}', () =>
    expect(translateError(tEn, apiError('vehicle.limit_reached', { limit: 5 }))).toBe(
      'You reached the limit of 5 vehicles.',
    ));

  test('a placeholder with no value fails instead of leaking a raw token', () =>
    expect(() => translateError(tEn, apiError('rate_limit.exceeded'))).toThrow(/no value passed for \{seconds\}/));
});
