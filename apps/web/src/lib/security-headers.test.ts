import { describe, expect, test } from 'vitest';
import { securityHeaders } from './security-headers';

const valueOf = (key: string) => securityHeaders.find((header) => header.key === key)?.value;

describe('securityHeaders', () => {
  test('sets nosniff, referrer policy and the frame/base/form/object CSP', () => {
    expect(valueOf('X-Content-Type-Options')).toBe('nosniff');
    expect(valueOf('Referrer-Policy')).toBe('strict-origin-when-cross-origin');
    expect(valueOf('Content-Security-Policy')).toBe(
      "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
    );
    expect(securityHeaders).toHaveLength(3);
  });
});
