import { describe, expect, test } from 'vitest';
import {
  availabilityReducedFixture,
  lotFixture,
  spacesFixture,
  sseText,
} from '../fixtures';
import { availabilitySchema } from './availability';
import { lotSchema } from './lot';
import {
  CAR_ID_PATTERN,
  injectCarSchema,
  simulatorStatusSchema,
  startScenarioSchema,
} from './simulator';
import { SPACE_CODE_PATTERN, spaceStateSchema } from './spaces';

function messages(result: { error?: { issues: { path: PropertyKey[]; message: string }[] } }) {
  return result.error?.issues.map((i) => [String(i.path[0]), i.message]);
}

describe('response schemas', () => {
  test('lotSchema parses the fixture and rejects an extra key', () => {
    expect(lotSchema.safeParse(lotFixture).success).toBe(true);
    expect(lotSchema.safeParse({ ...lotFixture, extra: 1 }).success).toBe(false);
  });

  test('availabilitySchema rejects a string version', () => {
    expect(availabilitySchema.safeParse({ ...availabilityReducedFixture, version: '121' }).success).toBe(false);
  });

  test('spaceStateSchema rejects an unknown state', () => {
    expect(spaceStateSchema.safeParse({ code: 'E001', state: 'parked' }).success).toBe(false);
  });

  test('simulatorStatusSchema accepts the empty scenario without started_at', () => {
    const result = simulatorStatusSchema.safeParse({
      scenario: '',
      running: false,
      seed: 0,
      speed: 0,
      virtual_now: '2026-11-15T14:47:00Z',
      cars_inside: 0,
      reached_full: false,
    });
    expect(result.success).toBe(true);
  });
});

describe('request schemas', () => {
  test('startScenarioSchema bounds speed', () => {
    expect(messages(startScenarioSchema.safeParse({ scenario: 'idle', speed: 0 }))).toEqual([['speed', 'min:1']]);
    expect(messages(startScenarioSchema.safeParse({ scenario: 'idle', speed: 121 }))).toEqual([['speed', 'max:120']]);
    expect(messages(startScenarioSchema.safeParse({ scenario: 'idle', speed: 2.5 }))).toEqual([['speed', 'integer']]);
  });

  test('startScenarioSchema offers only console scenarios', () => {
    expect(messages(startScenarioSchema.safeParse({ scenario: 'reservation_arrivals', speed: 30 }))).toEqual([['scenario', 'oneof']]);
  });

  test('injectCarSchema requires a card for employee_card', () => {
    const result = injectCarSchema.safeParse({ credential: 'employee_card' });
    expect(result.error?.issues.map((i) => [i.path, i.message])).toEqual([[['card'], 'required']]);
  });

  test('injectCarSchema drops the card for a ticket', () => {
    const result = injectCarSchema.safeParse({ credential: 'ticket', card: 'abc' });
    expect(result.success).toBe(true);
    expect(result.data).not.toHaveProperty('card');
    expect(result.data?.hold).toBe(false);
  });

  test('injectCarSchema trims the card', () => {
    expect(injectCarSchema.parse({ credential: 'employee_card', card: '  EMP-1 ' }).card).toBe('EMP-1');
  });

  test('injectCarSchema bounds dwell minutes', () => {
    expect(messages(injectCarSchema.safeParse({ credential: 'ticket', dwell_minutes: 0 }))).toEqual([['dwell_minutes', 'min:1']]);
    expect(messages(injectCarSchema.safeParse({ credential: 'ticket', dwell_minutes: 601 }))).toEqual([['dwell_minutes', 'max:600']]);
  });
});

test('car id and space code patterns', () => {
  expect(CAR_ID_PATTERN.test('car-000123')).toBe(true);
  expect(CAR_ID_PATTERN.test('car-12')).toBe(false);
  expect(CAR_ID_PATTERN.test('CAR-000123')).toBe(false);
  expect(SPACE_CODE_PATTERN.test('E017')).toBe(true);
  expect(SPACE_CODE_PATTERN.test('E1000')).toBe(false);
  expect(SPACE_CODE_PATTERN.test('e017')).toBe(false);
});

test('spacesFixture is E001..E280 in order with 211 free, 66 occupied, 3 out of service', () => {
  const { spaces } = spacesFixture;
  expect(spaces.map((s) => s.code)).toEqual(Array.from({ length: 280 }, (_, i) => `E${String(i + 1).padStart(3, '0')}`));
  const count = (state: string) => spaces.filter((s) => s.state === state).length;
  expect([count('free'), count('occupied'), count('out_of_service')]).toEqual([211, 66, 3]);
});

test('sseText writes event, id and data and ends with a blank line', () => {
  expect(sseText('heartbeat', { as_of: 'x' })).toBe('event: heartbeat\ndata: {"as_of":"x"}\n\n');
  expect(sseText('availability', { a: 1 }, 7)).toBe('event: availability\nid: 7\ndata: {"a":1}\n\n');
});
