import { successBody, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http, session } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { ProfileScreen } from './ProfileScreen';

const mockRouter = { push: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { logout: jest.fn(), signOutLocal: jest.fn() },
}));
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

const request = jest.mocked(http.request);
const logout = jest.mocked(session.logout);

beforeEach(() => {
  mockRouter.push.mockReset();
  request.mockReset();
  logout.mockReset().mockResolvedValue('revoked');
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthStore.getState().setSession({ accessToken: 'a', user: userFixture, legalPending: false });
  useSettingsStore.setState(useSettingsStore.getInitialState(), true);
});

async function loaded() {
  request.mockResolvedValueOnce(reply(200, successBody(userFixture)));
  await renderScreen(<ProfileScreen />);
  await waitFor(() => expect(screen.getByLabelText('First name').props.value).toBe('Ana'));
}

test('email is read-only; name and phone patch', async () => {
  await loaded();
  const email = screen.getByLabelText('Email');
  expect(email.props.value).toBe('ana@example.do');
  expect(email.props.editable).toBe(false);
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ method: 'GET', path: '/v1/me', auth: true }));

  const updated = { ...userFixture, first_name: 'Anita', phone: '+18095559999' };
  request.mockResolvedValueOnce(reply(200, successBody(updated)));
  await fireEvent.changeText(screen.getByLabelText('First name'), 'Anita');
  await fireEvent.changeText(screen.getByLabelText('Phone (optional)'), '+1 809-555-9999');
  await fireEvent.press(screen.getByRole('button', { name: 'Save changes' }));

  expect(await screen.findByText('Changes saved.')).toBeTruthy();
  // Only what changed goes out, and the phone is normalized by the shared schema.
  expect(request).toHaveBeenLastCalledWith(
    expect.objectContaining({ method: 'PATCH', path: '/v1/me', body: { first_name: 'Anita', phone: '+18095559999' }, auth: true }),
  );
  expect(useAuthStore.getState().user?.first_name).toBe('Anita');
});

test('language switch persists and patches', async () => {
  await loaded();
  request.mockResolvedValue(reply(200, successBody(userFixture)));

  await fireEvent.press(screen.getByRole('button', { name: 'Español' }));

  await waitFor(() =>
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ method: 'PATCH', path: '/v1/me', body: { locale: 'es-DO' }, auth: true })),
  );
  expect(useSettingsStore.getState().language).toBe('es-DO');
});

test('links to forgot password', async () => {
  await loaded();
  await fireEvent.press(screen.getByRole('link', { name: 'Change my password' }));
  expect(mockRouter.push).toHaveBeenCalledWith('/forgot-password');
});

test('delete account opens its own screen', async () => {
  await loaded();
  await fireEvent.press(screen.getByRole('button', { name: 'Delete my account' }));
  expect(mockRouter.push).toHaveBeenCalledWith('/profile/delete');
});

test('log out and log out everywhere go through the session service', async () => {
  await loaded();

  await fireEvent.press(screen.getByRole('button', { name: 'Log out' }));
  await waitFor(() => expect(logout).toHaveBeenCalledWith('this'));

  await fireEvent.press(screen.getByRole('button', { name: 'Log out on all devices' }));
  await waitFor(() => expect(logout).toHaveBeenCalledWith('all'));
  expect(logout).toHaveBeenCalledTimes(2);
});

test('a double tap on log out calls the service once', async () => {
  await loaded();
  logout.mockReturnValue(new Promise(() => undefined));

  const button = screen.getByRole('button', { name: 'Log out' });
  await fireEvent.press(button);
  await fireEvent.press(button);

  expect(logout).toHaveBeenCalledTimes(1);
});
