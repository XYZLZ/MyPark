import type { User } from '@mypark/contracts';

import { useAuthStore } from './auth';

const user = { id: 'u1', email: 'a@b.co' } as unknown as User;

beforeEach(() => useAuthStore.setState(useAuthStore.getInitialState(), true));

test('starts restoring with no tokens', () => {
  expect(useAuthStore.getState()).toMatchObject({
    status: 'restoring',
    accessToken: null,
    user: null,
    legalPending: false,
    notice: null,
  });
});

test('setSession signs in and keeps the access token in memory', () => {
  useAuthStore.getState().setSession({ accessToken: 'tok', user, legalPending: true });
  expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'tok', user, legalPending: true });
});

test('clearSession wipes tokens and user and records the notice', () => {
  useAuthStore.getState().setSession({ accessToken: 'tok', user, legalPending: true });
  useAuthStore.getState().clearSession('session_ended');
  expect(useAuthStore.getState()).toMatchObject({
    status: 'signedOut',
    accessToken: null,
    user: null,
    legalPending: false,
    notice: 'session_ended',
  });
  useAuthStore.getState().clearNotice();
  expect(useAuthStore.getState().notice).toBeNull();
});

test('clearSession without a notice leaves none', () => {
  useAuthStore.getState().clearSession();
  expect(useAuthStore.getState().notice).toBeNull();
});
