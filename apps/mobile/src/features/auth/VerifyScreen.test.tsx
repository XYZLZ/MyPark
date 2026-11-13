import { errorBody, successBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http } from '@/services/runtime';
import { useAuthFlowStore } from '@/store/auth-flow';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { VerifyScreen } from './VerifyScreen';

const mockReplace = jest.fn();
const mockRouter = { push: jest.fn(), replace: mockReplace };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

beforeEach(() => {
  mockReplace.mockReset();
  request.mockReset();
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
  useAuthFlowStore.getState().setEmail('ana@example.do');
});

test('bad code is a translated field error with resend', async () => {
  request.mockResolvedValue(reply(400, errorBody('auth.code_invalid')));
  await renderScreen(<VerifyScreen />);

  await fireEvent.changeText(screen.getByLabelText('Verification code'), '123 456');
  await fireEvent.press(screen.getByRole('button', { name: 'Verify' }));

  expect(await screen.findByText('The code is not valid or has expired.')).toBeTruthy();
  expect(screen.getByLabelText('Verification code, The code is not valid or has expired.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Send a new code' })).toBeTruthy();
  // A pasted code loses its spaces before it is sent.
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ body: { email: 'ana@example.do', code: '123456' } }));
});

test('verified goes to login with the email and an empty password', async () => {
  request.mockResolvedValue(reply(204));
  await renderScreen(<VerifyScreen />);

  await fireEvent.changeText(screen.getByLabelText('Verification code'), '123456');
  await fireEvent.press(screen.getByRole('button', { name: 'Verify' }));

  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/login'));
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
  expect(JSON.stringify(useAuthFlowStore.getState())).not.toContain('password');
});

test.each([
  ['accepted', reply(202, successBody({ status: 'accepted' }))],
  ['an unknown account', reply(404, errorBody('user.not_found'))],
  ['an already verified account', reply(409, errorBody('user.already_verified'))],
])('resend shows the same message on every outcome: %s', async (_name, response) => {
  request.mockResolvedValue(response);
  await renderScreen(<VerifyScreen />);

  await fireEvent.press(screen.getByRole('button', { name: 'Send a new code' }));

  expect(await screen.findByText('If an account exists for that email, we sent a new code.')).toBeTruthy();
  expect(screen.queryByText('Something went wrong. Please try again.')).toBeNull();
});

test('resend is unavailable while the code is being verified', async () => {
  let finish: () => void = () => undefined;
  request.mockReturnValue(new Promise((resolve) => (finish = () => resolve(reply(204)))));
  await renderScreen(<VerifyScreen />);

  await fireEvent.changeText(screen.getByLabelText('Verification code'), '123456');
  await fireEvent.press(screen.getByRole('button', { name: 'Verify' }));
  await fireEvent.press(screen.getByRole('button', { name: 'Send a new code' }));

  expect(screen.getByRole('button', { name: 'Send a new code' }).props.accessibilityState.disabled).toBe(true);
  expect(request).toHaveBeenCalledTimes(1);
  finish();
  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/login'));
});

test('resend still reports a rate limit', async () => {
  request.mockResolvedValue(reply(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 45 })));
  await renderScreen(<VerifyScreen />);

  await fireEvent.press(screen.getByRole('button', { name: 'Send a new code' }));

  expect(await screen.findByText('Too many attempts. Try again in 45 s.')).toBeTruthy();
});
