import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http, session } from '@/services/runtime';
import { useAuthFlowStore } from '@/store/auth-flow';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { ResetPasswordScreen } from './ResetPasswordScreen';

const mockReplace = jest.fn();
const mockRouter = { push: jest.fn(), replace: mockReplace };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { signOutLocal: jest.fn() },
}));

const request = jest.mocked(http.request);
const signOutLocal = jest.mocked(session.signOutLocal);

beforeEach(() => {
  mockReplace.mockReset();
  request.mockReset();
  signOutLocal.mockReset().mockResolvedValue(undefined);
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
  useAuthFlowStore.getState().setEmail('ana@example.do');
});

test('success signs out locally and goes to login pre-filled', async () => {
  request.mockResolvedValue(reply(204));
  await renderScreen(<ResetPasswordScreen />);

  await fireEvent.changeText(screen.getByLabelText('Verification code'), '123456');
  await fireEvent.changeText(screen.getByLabelText('New password'), 'another long password');
  await fireEvent.press(screen.getByRole('button', { name: 'Change password' }));

  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/login'));
  expect(signOutLocal).toHaveBeenCalledTimes(1);
  // The reset revoked every session on the server: the local one must be gone before the user moves on.
  expect(signOutLocal.mock.invocationCallOrder[0]).toBeLessThan(mockReplace.mock.invocationCallOrder[0]!);
  expect(request).toHaveBeenCalledWith(
    expect.objectContaining({
      path: '/v1/auth/password/reset',
      body: { email: 'ana@example.do', code: '123456', new_password: 'another long password' },
      timeoutMs: 20000,
    }),
  );
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
  expect(JSON.stringify(useAuthFlowStore.getState())).not.toContain('another long password');
});

test('a rejected reset keeps the session', async () => {
  request.mockResolvedValue(reply(400, { error: { code: 'auth.code_invalid', message: 'x', details: {} }, meta: { request_id: 'r' } }));
  await renderScreen(<ResetPasswordScreen />);

  await fireEvent.changeText(screen.getByLabelText('Verification code'), '123456');
  await fireEvent.changeText(screen.getByLabelText('New password'), 'another long password');
  await fireEvent.press(screen.getByRole('button', { name: 'Change password' }));

  expect(await screen.findByText('The code is not valid or has expired.')).toBeTruthy();
  expect(signOutLocal).not.toHaveBeenCalled();
  expect(mockReplace).not.toHaveBeenCalled();
});
