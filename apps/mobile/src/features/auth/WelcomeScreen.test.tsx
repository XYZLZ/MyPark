import { fireEvent, screen } from '@testing-library/react-native';

import { changeLanguage } from '@/features/session/change-language';
import { useAuthStore } from '@/store/auth';
import { renderScreen } from '@/testing/render';

import { WelcomeScreen } from './WelcomeScreen';

const mockPush = jest.fn();
const mockRouter = { push: mockPush, replace: jest.fn() };
const mockRedirect = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => mockRouter,
  Redirect: ({ href }: { href: string }) => {
    mockRedirect(href);
    return null;
  },
}));
jest.mock('@/features/session/change-language', () => ({ changeLanguage: jest.fn() }));

beforeEach(() => {
  mockPush.mockReset();
  mockRedirect.mockReset();
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  jest.mocked(changeLanguage).mockReset();
});

test('offers Sign up, Log in and the language switch, with no carousel', async () => {
  await renderScreen(<WelcomeScreen />);

  // Exactly these four controls: no pager dots, no "next" or "skip".
  expect(screen.getAllByRole('button').map((button) => button.props.accessibilityLabel)).toEqual([
    'Español',
    'English',
    'Sign up',
    'Log in',
  ]);

  await fireEvent.press(screen.getByRole('button', { name: 'Sign up' }));
  expect(mockPush).toHaveBeenLastCalledWith('/register');
  await fireEvent.press(screen.getByRole('button', { name: 'Log in' }));
  expect(mockPush).toHaveBeenLastCalledWith('/login');

  await fireEvent.press(screen.getByRole('button', { name: 'Español' }));
  expect(changeLanguage).toHaveBeenCalledWith('es-DO');
});

test('a signed-out start with no notice stays on Welcome', async () => {
  await renderScreen(<WelcomeScreen />);
  expect(mockRedirect).not.toHaveBeenCalled();
});

test.each(['session_ended', 'admin_web_only'] as const)('the %s notice sends the user to Login', async (notice) => {
  useAuthStore.getState().clearSession(notice);
  await renderScreen(<WelcomeScreen />);
  expect(mockRedirect).toHaveBeenCalledWith('/login');
  expect(screen.queryByRole('button', { name: 'Sign up' })).toBeNull();
});
