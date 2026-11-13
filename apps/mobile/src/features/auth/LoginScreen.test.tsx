import { adminFixture, errorBody, successBody, tokenResponseFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http, session } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useAuthFlowStore } from '@/store/auth-flow';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { LoginScreen } from './LoginScreen';

const mockPush = jest.fn();
const mockReplace = jest.fn();
const mockRouter = { push: mockPush, replace: mockReplace };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { applyTokens: jest.fn() },
}));

const request = jest.mocked(http.request);
const applyTokens = jest.mocked(session.applyTokens);

const PASSWORD = 'hunter2-correct-horse';

beforeEach(() => {
  mockPush.mockReset();
  mockReplace.mockReset();
  request.mockReset();
  applyTokens.mockReset().mockResolvedValue(undefined);
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
});

async function submit(email = 'ana@example.do') {
  await fireEvent.changeText(screen.getByLabelText('Email'), email);
  await fireEvent.changeText(screen.getByLabelText('Password'), PASSWORD);
  await fireEvent.press(screen.getByRole('button', { name: 'Log in' }));
}

test('success applies tokens and sets the language from the user', async () => {
  request.mockResolvedValue(reply(200, successBody(tokenResponseFixture)));
  await renderScreen(<LoginScreen />);

  await submit();

  // The session service applies `user.locale` itself (Task 9); the screen hands it the whole response.
  await waitFor(() => expect(applyTokens).toHaveBeenCalledWith(tokenResponseFixture));
  expect(tokenResponseFixture.user.locale).toBe('es-DO');
  expect(request).toHaveBeenCalledWith(
    expect.objectContaining({ path: '/v1/auth/login', body: { email: 'ana@example.do', password: PASSWORD, client_kind: 'mobile' } }),
  );
});

test('login with an admin account shows the web-admin notice and stays signed out', async () => {
  request.mockResolvedValue(reply(200, successBody({ ...tokenResponseFixture, user: adminFixture })));
  // What the real session service does for an admin (ruling T9).
  applyTokens.mockImplementation(async () => useAuthStore.getState().clearSession('admin_web_only'));
  await renderScreen(<LoginScreen />);

  await submit('admin@example.do');

  expect(await screen.findByText('Administrator accounts use the web admin area. This app is for drivers.')).toBeTruthy();
  expect(useAuthStore.getState().status).toBe('signedOut');
  expect(mockPush).not.toHaveBeenCalled();
  expect(mockReplace).not.toHaveBeenCalled();
  // The admin still sees which account was refused; only the password is gone.
  expect(screen.getByLabelText('Email').props.value).toBe('admin@example.do');
  expect(screen.getByLabelText('Password').props.value).toBe('');
});

test('wrong credentials show one banner', async () => {
  request.mockResolvedValue(reply(401, errorBody('auth.invalid_credentials')));
  await renderScreen(<LoginScreen />);

  await submit();

  expect(await screen.findByText('The email or password is not correct.')).toBeTruthy();
  expect(screen.getAllByTestId('banner')).toHaveLength(1);
  expect(applyTokens).not.toHaveBeenCalled();
});

test('email_not_verified goes to verify keeping only the email', async () => {
  request.mockResolvedValue(reply(403, errorBody('auth.email_not_verified')));
  await renderScreen(<LoginScreen />);

  await submit();

  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/verify'));
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
  expect(screen.queryAllByTestId('banner')).toHaveLength(0);
  expect(screen.getByLabelText('Password').props.value).toBe('');
});

test('shows the session-ended banner once', async () => {
  useAuthStore.getState().clearSession('session_ended');
  await renderScreen(<LoginScreen />, { language: 'es-DO' });

  expect(screen.getByText('Tu sesión terminó. Inicia sesión de nuevo.')).toBeTruthy();
  expect(screen.getAllByTestId('banner')).toHaveLength(1);

  // Leaving the screen consumes the notice, so it does not come back on the next visit.
  await screen.unmount();
  expect(useAuthStore.getState().notice).toBeNull();
});

test('pre-fills the email from authFlow, password empty', async () => {
  useAuthFlowStore.getState().setEmail('ana@example.do');
  await renderScreen(<LoginScreen />);

  expect(screen.getByLabelText('Email').props.value).toBe('ana@example.do');
  expect(screen.getByLabelText('Password').props.value).toBe('');
});

test('429 shows the wait and disables submit', async () => {
  request.mockResolvedValue(reply(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 30 })));
  await renderScreen(<LoginScreen />);

  await submit();

  expect(await screen.findByText('Too many attempts. Try again in 30 s.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Log in' }).props.accessibilityState.disabled).toBe(true);
});

test('the password never crosses screens', async () => {
  request.mockResolvedValue(reply(403, errorBody('auth.email_not_verified')));
  await renderScreen(<LoginScreen />);

  await submit();

  await waitFor(() => expect(mockPush).toHaveBeenCalled());
  expect(JSON.stringify(useAuthFlowStore.getState())).not.toContain(PASSWORD);
  expect(JSON.stringify([...mockPush.mock.calls, ...mockReplace.mock.calls])).not.toContain(PASSWORD);
});

test('double tap on log in sends one request', async () => {
  let finish: () => void = () => undefined;
  request.mockReturnValue(new Promise((resolve) => (finish = () => resolve(reply(200, successBody(tokenResponseFixture))))));
  await renderScreen(<LoginScreen />);

  await fireEvent.changeText(screen.getByLabelText('Email'), 'ana@example.do');
  await fireEvent.changeText(screen.getByLabelText('Password'), PASSWORD);
  const button = screen.getByRole('button', { name: 'Log in' });
  await fireEvent.press(button);
  await fireEvent.press(button);
  finish();

  await waitFor(() => expect(applyTokens).toHaveBeenCalledTimes(1));
  expect(request).toHaveBeenCalledTimes(1);
});
