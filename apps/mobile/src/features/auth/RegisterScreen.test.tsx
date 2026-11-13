import { errorBody, legalCurrentFixture, successBody, validationBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http } from '@/services/runtime';
import { useAuthFlowStore } from '@/store/auth-flow';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { RegisterScreen } from './RegisterScreen';

const mockPush = jest.fn();
const mockRouter = { push: mockPush, replace: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

const registerCalls = () => request.mock.calls.filter(([req]) => req.path === '/v1/auth/register');

beforeEach(() => {
  mockPush.mockReset();
  request.mockReset();
  request.mockImplementation(async (req) => {
    if (req.path === '/v1/legal/current') return reply(200, successBody(legalCurrentFixture));
    return reply(202, successBody({ status: 'verification_pending' }));
  });
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
});

async function fillForm({ accept = true } = {}) {
  await fireEvent.changeText(await screen.findByLabelText('First name'), 'Ana');
  await fireEvent.changeText(screen.getByLabelText('Last name'), 'Perez');
  await fireEvent.changeText(screen.getByLabelText('Email'), 'ana@example.do');
  await fireEvent.changeText(screen.getByLabelText('Password'), 'correct horse battery');
  if (accept) await fireEvent.press(screen.getByRole('checkbox'));
  await fireEvent.press(screen.getByRole('button', { name: 'Sign up' }));
}

test('sends legal versions, the current language and empty phone for +1', async () => {
  await renderScreen(<RegisterScreen />);

  await fillForm();

  await waitFor(() => expect(registerCalls()).toHaveLength(1));
  const [req] = registerCalls()[0]!;
  // The phone key is absent, not empty: the form's `+1` pre-fill means "no phone".
  expect(req.body).toEqual({
    first_name: 'Ana',
    last_name: 'Perez',
    email: 'ana@example.do',
    password: 'correct horse battery',
    locale: 'en',
    accepted_terms_version: legalCurrentFixture.terms.version,
    accepted_privacy_version: legalCurrentFixture.privacy.version,
  });
  expect(req.timeoutMs).toBe(20000);
});

test('field error mapping includes password_common', async () => {
  request.mockImplementation(async (req) =>
    req.path === '/v1/legal/current'
      ? reply(200, successBody(legalCurrentFixture))
      : reply(422, validationBody([{ field: 'password', rule: 'password_common' }])),
  );
  await renderScreen(<RegisterScreen />);

  await fillForm();

  expect(await screen.findByText('That password is too common. Choose another.')).toBeTruthy();
  expect(screen.getByLabelText('Password, That password is too common. Choose another.')).toBeTruthy();
});

test('requires the terms checkbox', async () => {
  await renderScreen(<RegisterScreen />);

  await fillForm({ accept: false });

  expect(await screen.findByText('This field is required.')).toBeTruthy();
  expect(registerCalls()).toHaveLength(0);
});

test('legal fetch failure shows retry', async () => {
  request.mockRejectedValueOnce(Object.assign(new Error('network'), { code: 'client.network' }));
  await renderScreen(<RegisterScreen />);

  expect(await screen.findByText('Sign-up is unavailable')).toBeTruthy();
  expect(screen.queryByLabelText('Email')).toBeNull();

  await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));

  expect(await screen.findByLabelText('Email')).toBeTruthy();
});

test('success goes to verify and resets the form', async () => {
  await renderScreen(<RegisterScreen />);

  await fillForm();

  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/verify'));
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
  expect(screen.getByLabelText('Password').props.value).toBe('');
  expect(screen.getByLabelText('First name').props.value).toBe('');
});

test('a 409 for stale legal versions asks for a fresh acceptance', async () => {
  let attempt = 0;
  request.mockImplementation(async (req) => {
    if (req.path === '/v1/legal/current') return reply(200, successBody(legalCurrentFixture));
    attempt += 1;
    return reply(409, errorBody('legal.version_outdated'));
  });
  await renderScreen(<RegisterScreen />);

  await fillForm();

  expect(await screen.findByText('The legal texts changed. Read and accept them again.')).toBeTruthy();
  expect(attempt).toBe(1);
  expect(screen.getByRole('checkbox').props.accessibilityState.checked).toBe(false);
});
