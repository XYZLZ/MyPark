import * as SecureStore from 'expo-secure-store';

import { REFRESH_TOKEN_KEY, tokenStorage } from './tokenStorage';

jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn(),
  deleteItemAsync: jest.fn(),
  WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'THIS_DEVICE_ONLY',
}));

const store = jest.mocked(SecureStore);
const OPTIONS = { keychainAccessible: 'THIS_DEVICE_ONLY' };

beforeEach(() => jest.clearAllMocks());

test('uses mypark.refresh_token for get, set and remove', async () => {
  expect(REFRESH_TOKEN_KEY).toBe('mypark.refresh_token');
  store.getItemAsync.mockResolvedValue('abc');
  await expect(tokenStorage.get()).resolves.toBe('abc');
  await tokenStorage.set('def');
  await tokenStorage.remove();
  expect(store.getItemAsync).toHaveBeenCalledWith('mypark.refresh_token', OPTIONS);
  expect(store.setItemAsync).toHaveBeenCalledWith('mypark.refresh_token', 'def', OPTIONS);
  expect(store.deleteItemAsync).toHaveBeenCalledWith('mypark.refresh_token', OPTIONS);
});

test('a failed write rejects so the caller can handle it', async () => {
  store.setItemAsync.mockRejectedValue(new Error('keystore locked'));
  await expect(tokenStorage.set('x')).rejects.toThrow('keystore locked');
});
