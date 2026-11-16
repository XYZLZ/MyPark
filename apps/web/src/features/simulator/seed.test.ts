import { startScenarioSchema } from '@mypark/contracts';
import { describe, expect, test } from 'vitest';
import { extractRawSeed, startFormSchema, toStartBody } from './seed';

function issueOf(values: Record<string, unknown>): string[] {
  const result = startFormSchema.safeParse({ scenario: 'weekday', speed: 30, seed: '', start_at: '', ...values });
  return result.success ? [] : result.error.issues.map((issue) => issue.message);
}

describe('seed', () => {
  test('a seed above 2^53 round-trips exactly', () => {
    expect(extractRawSeed('{"data":{"seed":9007199254740993,"speed":30}}')).toBe('9007199254740993');
    expect(toStartBody({ scenario: 'weekday', speed: 30, seed: '9007199254740993', start_at: '' })).toBe(
      '{"scenario":"weekday","speed":30,"seed":9007199254740993}',
    );
  });

  test('extractRawSeed gives null when there is no seed', () => {
    expect(extractRawSeed('{"data":{}}')).toBeNull();
  });

  test('start_at is sent as Santo Domingo RFC 3339', () => {
    expect(toStartBody({ scenario: 'weekday', speed: 30, seed: '', start_at: '2026-11-24T07:00' })).toContain(
      '"start_at":"2026-11-24T07:00:00-04:00"',
    );
  });

  test('a seed with leading zeros is normalized, so the JSON stays valid', () => {
    expect(issueOf({ seed: '007' })).toEqual([]);
    expect(toStartBody({ scenario: 'idle', speed: 1, seed: '007', start_at: '' })).toBe('{"scenario":"idle","speed":1,"seed":7}');
  });

  test('start_at round trips to the same instant through the contract schema', () => {
    const body = JSON.parse(toStartBody({ scenario: 'idle', speed: 1, seed: '', start_at: '2026-11-24T07:00' })) as unknown;
    const parsed = startScenarioSchema.parse(body);
    expect(new Date(parsed.start_at!).toISOString()).toBe('2026-11-24T11:00:00.000Z');
  });

  test('empty seed and start_at are omitted', () => {
    expect(toStartBody({ scenario: 'idle', speed: 1, seed: '', start_at: '' })).toBe('{"scenario":"idle","speed":1}');
  });

  test('startFormSchema bounds seed and speed', () => {
    expect(issueOf({ seed: 'abc' })).toContain('integer');
    expect(issueOf({ seed: '9223372036854775808' })).toContain('max:9223372036854775807');
    expect(issueOf({ seed: '9223372036854775807' })).toEqual([]);
    expect(issueOf({ seed: '0' })).toEqual([]);
    expect(issueOf({ speed: 0 })).toContain('min:1');
    expect(issueOf({ speed: 121 })).toContain('max:120');
    expect(issueOf({ speed: 1.5 })).toContain('integer');
  });

  test('start_at must be a local date-time', () => {
    expect(issueOf({ start_at: 'tomorrow' })).toContain('pattern');
    expect(issueOf({ start_at: '2026-11-24T07:00' })).toEqual([]);
  });

  test('the scenario must be one the console offers', () => {
    expect(issueOf({ scenario: 'no_show' })).toContain('oneof');
  });
});
