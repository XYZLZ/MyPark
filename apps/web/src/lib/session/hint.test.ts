// @vitest-environment node
import { describe, expect, test } from 'vitest';
import { hasSessionHint } from './hint';

const store = (...names: string[]) => ({ has: (name: string) => names.includes(name) });

describe('hasSessionHint', () => {
  test.each([[['mp_access']], [['mp_refresh']], [['mp_access', 'mp_refresh']]])('is true with %o', (names) => {
    expect(hasSessionHint(store(...names))).toBe(true);
  });

  test.each([[[]], [['mp_csrf']], [['mp_session_retry']]])('is false with %o', (names) => {
    expect(hasSessionHint(store(...names))).toBe(false);
  });
});
