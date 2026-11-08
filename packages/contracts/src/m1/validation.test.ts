import { describe, expect, test } from 'vitest';
import authHttp from '../../../../apps/server/internal/features/auth/http.go?raw';
import adminsHttp from '../../../../apps/server/internal/features/users/admins_http.go?raw';
import usersHttp from '../../../../apps/server/internal/features/users/http.go?raw';
import vehiclePlate from '../../../../apps/server/internal/features/vehicles/plate.go?raw';
import vehiclesHttp from '../../../../apps/server/internal/features/vehicles/http.go?raw';
import validationGo from '../../../../apps/server/internal/infrastructure/validation/validation.go?raw';
import { CLIENT_ERROR_CODES, VALIDATION_RULES, parseFieldErrors, parseIssueMessage } from './validation';

/** Every rule name the Go server can put in `details.fields[].rule`, read from its sources. */
function serverRules(): string[] {
  const rules = new Set<string>();
  const sources = [authHttp, adminsHttp, usersHttp, vehiclesHttp, vehiclePlate, validationGo];
  for (const src of sources) {
    for (const [, tags] of src.matchAll(/validate:"([^"]+)"/g)) {
      for (const tag of (tags ?? '').split(',')) {
        const name = tag.split('=')[0] ?? '';
        if (name !== 'omitempty' && name !== 'omitnil') rules.add(name);
      }
    }
    for (const [, name] of src.matchAll(/RegisterValidation\("([a-z0-9_]+)"/g)) rules.add(name ?? '');
    for (const [, name] of src.matchAll(/Rule:\s*"([a-z0-9_]+)"/g)) rules.add(name ?? '');
  }
  return [...rules].sort();
}

describe('VALIDATION_RULES', () => {
  test('VALIDATION_RULES holds every rule the server emits', () => {
    const emitted = serverRules();
    expect(emitted).toEqual(
      expect.arrayContaining(['required', 'email', 'min', 'max', 'oneof', 'phone', 'code6', 'password_common', 'plate']),
    );
    expect([...VALIDATION_RULES].filter((r) => r !== 'unknown').sort()).toEqual(emitted);
    expect(VALIDATION_RULES).toContain('unknown');
  });

  test('client-only error codes are network and timeout', () => {
    expect(CLIENT_ERROR_CODES).toEqual(['client.network', 'client.timeout']);
  });
});

describe('parseIssueMessage', () => {
  test('parseIssueMessage splits rule and param', () => {
    expect(parseIssueMessage('min:10')).toEqual({ rule: 'min', param: '10' });
    expect(parseIssueMessage('oneof:es-DO en')).toEqual({ rule: 'oneof', param: 'es-DO en' });
    expect(parseIssueMessage('required')).toEqual({ rule: 'required' });
  });

  test('parseIssueMessage maps an unlisted rule to unknown', () => {
    expect(parseIssueMessage('e164')).toEqual({ rule: 'unknown' });
  });
});

describe('parseFieldErrors', () => {
  test('parseFieldErrors drops malformed entries', () => {
    const details = {
      fields: [
        { field: 'code', rule: 'code6', param: '' },
        { field: 'password', rule: 'min', param: '10' },
        { field: 'x' },
        'nope',
        null,
        { field: 1, rule: 'min' },
      ],
    };
    expect(parseFieldErrors(details)).toEqual([
      { field: 'code', rule: 'code6' },
      { field: 'password', rule: 'min', param: '10' },
    ]);
    expect(parseFieldErrors({})).toEqual([]);
    expect(parseFieldErrors({ fields: 'bad' })).toEqual([]);
  });
});
