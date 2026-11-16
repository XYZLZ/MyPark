import { afterEach, describe, expect, test, vi } from 'vitest';
import { formatMoney } from './money';
import { formatClock, formatLogTime } from './time';

// es-DO times use narrow no-break spaces (`a. m.`); compare on plain spaces.
const normalize = (text: string) => text.replace(/[\s  ]+/g, ' ');
const fallback = (amount: string) => `RD$${amount}`;

describe('formatMoney', () => {
  test('formatMoney prints RD$ in both locales', () => {
    expect(formatMoney(5000, 'es-DO', fallback)).toBe('RD$50.00');
    expect(formatMoney(5000, 'en', fallback)).toBe('RD$50.00');
    expect(formatMoney(40000, 'es-DO', fallback)).toBe('RD$400.00');
  });

  test('formatMoney uses the fallback with locale digits when the runtime prints DOP', () => {
    const seen: string[] = [];
    const record = (amount: string) => {
      seen.push(amount);
      return `RD$${amount}`;
    };
    const original = Intl.NumberFormat;
    const fake = function (locale?: string | string[], options?: Intl.NumberFormatOptions) {
      if (options?.style === 'currency') return { format: (n: number) => `DOP ${n}` } as Intl.NumberFormat;
      return new original(locale, options);
    } as unknown as typeof Intl.NumberFormat;
    Intl.NumberFormat = fake;
    try {
      expect(formatMoney(123456, 'en', record)).toBe('RD$1,234.56');
      expect(formatMoney(5000, 'es-DO', record)).toBe('RD$50.00');
    } finally {
      Intl.NumberFormat = original;
    }
    expect(seen).toEqual(['1,234.56', '50.00']);
  });
});

describe('formatClock', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  test('formatClock ignores the machine time zone', () => {
    // Node re-reads TZ when it changes, so this holds even where the shell or
    // npm drops a TZ prefix (Git Bash on Windows). The guard proves it took.
    vi.stubEnv('TZ', 'Europe/Madrid');
    expect(new Date('2026-11-15T14:47:00Z').getHours()).toBe(15);
    expect(normalize(formatClock('2026-11-15T14:47:00Z', 'es-DO'))).toBe('10:47 a. m.');
    expect(normalize(formatClock('2026-11-15T14:47:00Z', 'en'))).toBe('10:47 AM');
    expect(normalize(formatClock(Date.parse('2026-11-15T14:47:00Z'), 'en'))).toBe('10:47 AM');
  });

  test('formatClock gives an empty string for an invalid time instead of throwing', () => {
    expect(formatClock('not a time', 'es-DO')).toBe('');
    expect(formatClock(Number.NaN, 'en')).toBe('');
  });
});

describe('formatLogTime', () => {
  test('is HH:mm:ss, 24 hours, in Santo Domingo, in both locales', () => {
    expect(formatLogTime('2026-11-15T18:32:05Z', 'es-DO')).toBe('14:32:05');
    expect(formatLogTime('2026-11-15T18:32:05Z', 'en')).toBe('14:32:05');
  });

  test('midnight is 00, not 24, and an invalid time gives an empty string', () => {
    expect(formatLogTime('2026-11-15T04:05:09Z', 'en')).toBe('00:05:09');
    expect(formatLogTime('nope', 'en')).toBe('');
  });
});
