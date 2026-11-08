import * as SecureStore from 'expo-secure-store';

import { LANGUAGE_KEY, hydrateSettings, useSettingsStore } from './settings';

jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn(),
  deleteItemAsync: jest.fn(),
}));

const store = jest.mocked(SecureStore);

beforeEach(() => {
  jest.resetAllMocks();
  store.setItemAsync.mockResolvedValue(undefined);
  store.deleteItemAsync.mockResolvedValue(undefined);
  useSettingsStore.setState(useSettingsStore.getInitialState(), true);
});

test('language persists', () => {
  expect(LANGUAGE_KEY).toBe('mypark.language');
  useSettingsStore.getState().setLanguage('en');
  expect(useSettingsStore.getState().language).toBe('en');
  expect(store.setItemAsync).toHaveBeenCalledWith('mypark.language', 'en');
});

test('hydrate restores a stored language', async () => {
  store.getItemAsync.mockResolvedValue('en');
  await hydrateSettings();
  expect(store.getItemAsync).toHaveBeenCalledWith('mypark.language');
  expect(useSettingsStore.getState()).toMatchObject({ language: 'en', hydrated: true });
});

test('hydrate ignores an unknown stored value', async () => {
  store.getItemAsync.mockResolvedValue('fr');
  await hydrateSettings();
  expect(useSettingsStore.getState()).toMatchObject({ language: null, hydrated: true });
});

test('hydrate survives a read failure', async () => {
  store.getItemAsync.mockRejectedValue(new Error('keystore locked'));
  await hydrateSettings();
  expect(useSettingsStore.getState()).toMatchObject({ language: null, hydrated: true });
});

test('a write failure keeps the in-memory language', async () => {
  store.setItemAsync.mockRejectedValue(new Error('keystore locked'));
  useSettingsStore.getState().setLanguage('en');
  await Promise.resolve();
  expect(useSettingsStore.getState().language).toBe('en');
});

test('a language chosen while hydrating is not overwritten by the stored one', async () => {
  let release: (value: string) => void = () => undefined;
  store.getItemAsync.mockReturnValue(new Promise<string>((resolve) => (release = resolve)));
  const hydrating = hydrateSettings();
  useSettingsStore.getState().setLanguage('es-DO');
  release('en');
  await hydrating;
  expect(useSettingsStore.getState().language).toBe('es-DO');
});
