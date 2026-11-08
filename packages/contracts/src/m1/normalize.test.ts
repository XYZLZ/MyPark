import { describe, expect, test } from 'vitest';
import { vehicleInputSchema } from './vehicle';
import { isValidPlate, normalizePhone, normalizePlate } from './normalize';

describe('plates', () => {
  test('normalizePlate uppercases and strips spaces and dashes', () => {
    expect(normalizePlate('a-1 23')).toBe('A123');
  });

  test('isValidPlate accepts 2 to 10 alphanumerics after normalizing', () => {
    expect(isValidPlate('A1')).toBe(true);
    expect(isValidPlate('A12345678901')).toBe(false);
  });

  test('rejects a plate made only of separators', () => {
    expect(isValidPlate('--')).toBe(false);
    const result = vehicleInputSchema.safeParse({ plate: '--', make: 'Kia', model: 'Rio', color: 'Red' });
    expect(result.success).toBe(false);
    expect(result.error?.issues.map((i) => [i.path[0], i.message])).toEqual([['plate', 'plate']]);
  });
});

describe('phones', () => {
  test('normalizes a spaced +1 phone', () => {
    expect(normalizePhone('+1 809-555-1234')).toBe('+18095551234');
  });

  test('a bare +1 with spaces is empty', () => {
    expect(normalizePhone('+1')).toBe('');
    expect(normalizePhone(' +1  ')).toBe('');
  });
});
