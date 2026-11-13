import { errorBody, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';
import { renderScreen } from '@/testing/render';

import { DeleteAccountForm } from './DeleteAccountForm';

// The real HTTP client and session service over a fake network, so "never refreshes" is observed, not assumed.
jest.mock('@/services/runtime', () => {
  const { createHttpClient } = jest.requireActual('@/services/httpClient');
  const { createAuthAdapter, createSessionService } = jest.requireActual('@/services/session');
  const { useAuthStore: store } = jest.requireActual('@/store/auth');
  const { QueryClient } = jest.requireActual('@tanstack/react-query');
  const fetchImpl = jest.fn();
  const realHttp = createHttpClient('http://api.test', fetchImpl);
  const storage = { get: jest.fn().mockResolvedValue('refresh-0'), set: jest.fn(), remove: jest.fn().mockResolvedValue(undefined) };
  const realSession = createSessionService({ http: realHttp, storage, store, queryClient: new QueryClient(), onUserLocale: jest.fn() });
  realHttp.setAuthAdapter(createAuthAdapter(realSession, store));
  return { http: realHttp, session: realSession, fetchImpl };
});
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

const { fetchImpl } = jest.requireMock('@/services/runtime') as { fetchImpl: jest.Mock };
const PASSWORD = 'hunter2-correct-horse';

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

const urls = () => fetchImpl.mock.calls.map(([url, init]) => `${init?.method ?? 'GET'} ${String(url).replace('http://api.test', '')}`);

beforeEach(() => {
  fetchImpl.mockReset();
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthStore.getState().setSession({ accessToken: 'access-0', user: userFixture, legalPending: false });
  useSettingsStore.setState(useSettingsStore.getInitialState(), true);
});

async function submit(password = PASSWORD) {
  await fireEvent.changeText(screen.getByLabelText('Password'), password);
  await fireEvent.press(screen.getByRole('button', { name: 'Delete my account' }));
}

test('explains consequences and requires the password', async () => {
  await renderScreen(<DeleteAccountForm onDeleted={jest.fn()} />);

  expect(screen.getByText('This permanently deletes your account, your personal data and your vehicles. It cannot be undone.')).toBeTruthy();
  await fireEvent.press(screen.getByRole('button', { name: 'Delete my account' }));

  expect(await screen.findByLabelText('Password, This field is required.')).toBeTruthy();
  expect(fetchImpl).not.toHaveBeenCalled();
});

test('password_incorrect lands on password and never refreshes', async () => {
  fetchImpl.mockResolvedValue(json(403, errorBody('auth.password_incorrect')));
  const onDeleted = jest.fn();
  await renderScreen(<DeleteAccountForm onDeleted={onDeleted} />);

  await submit('wrong-password');

  expect(await screen.findByLabelText('Password, The password is not correct.')).toBeTruthy();
  expect(urls()).toEqual(['DELETE /v1/me']);
  expect(useAuthStore.getState().status).toBe('signedIn');
  expect(onDeleted).not.toHaveBeenCalled();
});

test('success signs out and calls onDeleted', async () => {
  fetchImpl.mockResolvedValue(new Response(null, { status: 204 }));
  const onDeleted = jest.fn();
  await renderScreen(<DeleteAccountForm onDeleted={onDeleted} />);

  await submit();

  await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
  expect(urls()).toEqual(['DELETE /v1/me']);
  expect(JSON.parse(fetchImpl.mock.calls[0]![1].body)).toEqual({ password: PASSWORD });
  expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, notice: null });
});

test('the language setting survives account deletion', async () => {
  useSettingsStore.getState().setLanguage('en');
  fetchImpl.mockResolvedValue(new Response(null, { status: 204 }));
  await renderScreen(<DeleteAccountForm onDeleted={jest.fn()} />);

  await submit();

  await waitFor(() => expect(useAuthStore.getState().status).toBe('signedOut'));
  expect(useSettingsStore.getState().language).toBe('en');
});

test('a double tap sends one request', async () => {
  let finish: (res: Response) => void = () => undefined;
  fetchImpl.mockReturnValue(new Promise((resolve) => (finish = resolve)));
  await renderScreen(<DeleteAccountForm onDeleted={jest.fn()} />);

  await fireEvent.changeText(screen.getByLabelText('Password'), PASSWORD);
  const button = screen.getByRole('button', { name: 'Delete my account' });
  await fireEvent.press(button);
  await fireEvent.press(button);

  await waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(1));
  // Let the request finish so its timeout timer is cleared.
  finish(new Response(null, { status: 204 }));
  await waitFor(() => expect(useAuthStore.getState().status).toBe('signedOut'));
  expect(fetchImpl).toHaveBeenCalledTimes(1);
});
