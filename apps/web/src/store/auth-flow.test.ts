import { expect, test } from 'vitest';
import { createAuthFlowStore } from './auth-flow';

test('holds only the email and clears', () => {
  const store = createAuthFlowStore();
  const { setEmail, clear, ...data } = store.getState();
  expect(typeof setEmail).toBe('function');
  expect(typeof clear).toBe('function');
  expect(Object.keys(data)).toEqual(['email']);
  expect(data.email).toBe('');

  store.getState().setEmail('ana@example.do');
  expect(store.getState().email).toBe('ana@example.do');

  store.getState().clear();
  expect(store.getState().email).toBe('');
});

test('each store is independent', () => {
  const a = createAuthFlowStore();
  const b = createAuthFlowStore();
  a.getState().setEmail('a@example.do');
  expect(b.getState().email).toBe('');
});
