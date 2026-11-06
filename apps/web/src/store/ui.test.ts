import { describe, expect, test } from 'vitest';
import { createUiStore } from './ui';

describe('createUiStore', () => {
  test('starts on the system theme and updates through setTheme', () => {
    const store = createUiStore();
    expect(store.getState().theme).toBe('system');

    store.getState().setTheme('dark');
    expect(store.getState().theme).toBe('dark');
  });

  test('creates independent stores, so state never leaks between providers', () => {
    const a = createUiStore();
    const b = createUiStore({ theme: 'light' });

    a.getState().setTheme('dark');

    expect(a.getState().theme).toBe('dark');
    expect(b.getState().theme).toBe('light');
  });
});
