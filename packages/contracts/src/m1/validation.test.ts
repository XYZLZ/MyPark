import { describe, expect, test } from 'vitest';
import authHttp from '../../../../apps/server/internal/features/auth/http.go?raw';
import adminsHttp from '../../../../apps/server/internal/features/users/admins_http.go?raw';
import usersHttp from '../../../../apps/server/internal/features/users/http.go?raw';
import vehiclePlate from '../../../../apps/server/internal/features/vehicles/plate.go?raw';
import vehiclesHttp from '../../../../apps/server/internal/features/vehicles/http.go?raw';
import envelopeGo from '../../../../apps/server/internal/features/devices/envelope.go?raw';
import strictGo from '../../../../apps/server/internal/transport/http/httpx/strict.go?raw';
import validationGo from '../../../../apps/server/internal/infrastructure/validation/validation.go?raw';
import { CLIENT_ERROR_CODES, VALIDATION_RULES, parseFieldErrors, parseIssueMessage } from './validation';

/**
 * Rules only device firmware reads (the device event decoder, never a client
 * form), so they are deliberately not in VALIDATION_RULES. A new device rule
 * must be added here on purpose.
 */
const DEVICE_ONLY_RULES = ['uuid', 'datetime', 'future', 'format'];

/** Every rule name the Go server can put in `details.fields[].rule`, read from its sources. */
function serverRules(): string[] {
  const rules = new Set<string>();
  const sources = [authHttp, adminsHttp, usersHttp, vehiclesHttp, vehiclePlate, validationGo, strictGo, envelopeGo];
  for (const src of sources) {
    for (const [, tags] of src.matchAll(/validate:"([^"]+)"/g)) {
      for (const tag of (tags ?? '').split(',')) {
        const name = tag.split('=')[0] ?? '';
        if (name !== 'omitempty' && name !== 'omitnil') rules.add(name);
      }
    }
    for (const [, name] of src.matchAll(/RegisterValidation\("([a-z0-9_]+)"/g)) rules.add(name ?? '');
    for (const [, name] of src.matchAll(/\brule[A-Z][A-Za-z]*\s*=\s*"([a-z0-9_]+)"/g)) rules.add(name ?? '');
    for (const [, name] of src.matchAll(/Rule:\s*"([a-z0-9_]+)"/g)) rules.add(name ?? '');
  }
  return [...rules].sort();
}

function clientRules(): string[] {
  return serverRules().filter((r) => !DEVICE_ONLY_RULES.includes(r));
}

describe('VALIDATION_RULES', () => {
  test('VALIDATION_RULES holds every rule the server emits', () => {
    const emitted = clientRules();
    // Every device-only exemption must really be emitted, or it is stale.
    expect(serverRules()).toEqual(expect.arrayContaining(DEVICE_ONLY_RULES));
    expect(emitted).toEqual(
      expect.arrayContaining(['required', 'email', 'min', 'max', 'oneof', 'phone', 'code6', 'password_common', 'plate']),
    );
    expect([...VALIDATION_RULES].filter((r) => r !== 'unknown').sort()).toEqual(emitted);
    expect(VALIDATION_RULES).toContain('unknown_field');
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
