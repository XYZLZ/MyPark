import { describe, expect, test } from 'vitest';
import { safeNext } from './safe-next';

describe('safeNext', () => {
  test('accepts a same-origin path', () => {
    expect(safeNext('/es-DO/vehicles?x=1')).toBe('/es-DO/vehicles?x=1');
    expect(safeNext('/en/admin')).toBe('/en/admin');
  });

  test('rejects //evil.example', () => {
    expect(safeNext('//evil.example')).toBeNull();
    expect(safeNext('///evil.example')).toBeNull();
  });

  test.each([
    '/\\evil.example',
    '/%5Cevil.example',
    '/%5cevil.example',
    '/%2F%2Fevil.example',
    '/%252F%252Fevil.example',
    '/%09/evil.example',
    '/\t/evil.example',
  ])('safeNext rejects backslash and percent-encoded forms: %j', (value) => {
    expect(safeNext(value)).toBeNull();
  });

  test('rejects an absolute URL, an empty value and null', () => {
    expect(safeNext('https://evil.example/x')).toBeNull();
    expect(safeNext('javascript:alert(1)')).toBeNull();
    expect(safeNext('vehicles')).toBeNull();
    expect(safeNext('')).toBeNull();
    expect(safeNext(null)).toBeNull();
    expect(safeNext(undefined)).toBeNull();
  });

  test('rejects a value that is not valid percent-encoding', () => {
    expect(safeNext('/%E0%A4%A')).toBeNull();
  });
});
