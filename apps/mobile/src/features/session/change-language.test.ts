import { ApiError } from '@mypark/contracts';
import { userFixture } from '@mypark/contracts/fixtures';

import { http } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';

import { changeLanguage } from './change-language';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn(),
}));

const request = jest.mocked(http.request);

function signIn(legalPending: boolean) {
  useAuthStore.getState().setSession({ accessToken: 'a', user: userFixture, legalPending });
}

beforeEach(() => {
  request.mockReset();
  request.mockResolvedValue({ status: 200, body: undefined, retryAfter: null });
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useSettingsStore.setState(useSettingsStore.getInitialState(), true);
});

test('patches the account when signed in and not gated', async () => {
  signIn(false);
  await changeLanguage('en');
  expect(useSettingsStore.getState().language).toBe('en');
  expect(request).toHaveBeenCalledWith(
    expect.objectContaining({ method: 'PATCH', path: '/v1/me', body: { locale: 'en' }, auth: true }),
  );
});

test('does not patch behind the legal gate', async () => {
  signIn(true);
  await changeLanguage('en');
  expect(useSettingsStore.getState().language).toBe('en');
  expect(request).not.toHaveBeenCalled();
});

test('does not patch when signed out', async () => {
  await changeLanguage('en');
  expect(useSettingsStore.getState().language).toBe('en');
  expect(request).not.toHaveBeenCalled();
});

test('a failed patch keeps the local change', async () => {
  signIn(false);
  request.mockRejectedValue(new ApiError({ code: 'client.network', message: 'x' }));
  await expect(changeLanguage('en')).resolves.toBeUndefined();
  expect(useSettingsStore.getState().language).toBe('en');
});
