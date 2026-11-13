import { errorBody, successBody, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen } from '@testing-library/react-native';

import { http } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useAuthFlowStore } from '@/store/auth-flow';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { ForgotPasswordScreen } from './ForgotPasswordScreen';

const mockRouter = { push: jest.fn(), replace: jest.fn(), back: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

beforeEach(() => {
  request.mockReset();
  mockRouter.replace.mockReset();
  mockRouter.back.mockReset();
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
});

test.each([
  ['an existing account', reply(202, successBody({ status: 'accepted' }))],
  ['an unknown account', reply(404, errorBody('user.not_found'))],
])('shows the same sent message for %s', async (_name, response) => {
  request.mockResolvedValue(response);
  await renderScreen(<ForgotPasswordScreen />);

  await fireEvent.changeText(screen.getByLabelText('Email'), 'ana@example.do');
  await fireEvent.press(screen.getByRole('button', { name: 'Send code' }));

  expect(await screen.findByText('If an account exists for that email, we sent a code.')).toBeTruthy();
  expect(screen.queryAllByTestId('banner')).toHaveLength(1);
  // The reset screen opens with the email already filled in.
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
});

test('a rate limit is shown, not hidden', async () => {
  request.mockResolvedValue(reply(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 20 })));
  await renderScreen(<ForgotPasswordScreen />);

  await fireEvent.changeText(screen.getByLabelText('Email'), 'ana@example.do');
  await fireEvent.press(screen.getByRole('button', { name: 'Send code' }));

  expect(await screen.findByText('Too many attempts. Try again in 20 s.')).toBeTruthy();
});

describe('Back to log in', () => {
  test('signed out it goes to the login screen', async () => {
    await renderScreen(<ForgotPasswordScreen />);
    await fireEvent.press(screen.getByRole('link', { name: 'Back to log in' }));
    expect(mockRouter.replace).toHaveBeenCalledWith('/login');
    expect(mockRouter.back).not.toHaveBeenCalled();
  });

  test('signed in (from Profile) it goes back instead, since /login is behind a guard', async () => {
    useAuthStore.getState().setSession({ accessToken: 'a', user: userFixture, legalPending: false });
    await renderScreen(<ForgotPasswordScreen />);
    await fireEvent.press(screen.getByRole('link', { name: 'Back to log in' }));
    expect(mockRouter.back).toHaveBeenCalledTimes(1);
    expect(mockRouter.replace).not.toHaveBeenCalled();
  });
});
