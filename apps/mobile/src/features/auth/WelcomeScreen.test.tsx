import { ApiError } from '@mypark/contracts';
import { fireEvent, screen } from '@testing-library/react-native';

import { changeLanguage } from '@/features/session/change-language';
import { http } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { stubLotApi } from '@/testing/lot-api';
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
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));
// The stream has its own tests (LotLive.test.tsx); here the screen reads whatever the cache holds.
jest.mock('@/features/lot/live/useLiveLot', () => ({
  useLiveLot: () => ({ status: 'live', lastFrameAt: Date.parse('2026-11-15T14:47:00Z'), spacesReady: true }),
}));

const request = jest.mocked(http.request);

beforeEach(() => {
  mockPush.mockReset();
  mockRedirect.mockReset();
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  jest.mocked(changeLanguage).mockReset();
  request.mockReset();
  stubLotApi(request);
});

test('offers Log in, Sign up and the language switch, with no carousel', async () => {
  await renderScreen(<WelcomeScreen />);

  // Exactly these four controls: no pager dots, no "next" or "skip".
  expect(screen.getAllByRole('button').map((button) => button.props.accessibilityLabel)).toEqual([
    'Español',
    'English',
    'Log in',
    'Sign up',
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

test('a failed logout-all is explained once on Welcome', async () => {
  useAuthStore.getState().clearSession('logout_all_failed');
  await renderScreen(<WelcomeScreen />);
  expect(screen.getByText('We could not confirm that your other sessions were closed. If you are worried, change your password.')).toBeTruthy();
  expect(mockRedirect).not.toHaveBeenCalled();
});

test('leaving Welcome consumes the logout-all notice', async () => {
  useAuthStore.getState().clearSession('logout_all_failed');
  await renderScreen(<WelcomeScreen />);
  expect(useAuthStore.getState().notice).toBe('logout_all_failed');

  await screen.unmount();

  expect(useAuthStore.getState().notice).toBeNull();
});

test('Welcome renders the actions with the API down', async () => {
  request.mockRejectedValue(new ApiError({ code: 'client.network', message: 'offline', status: 0 }));
  await renderScreen(<WelcomeScreen />, { language: 'es-DO' });

  expect(await screen.findByRole('button', { name: 'Reintentar' })).toBeOnTheScreen();
  expect(screen.getByText('La disponibilidad no está cargando ahora')).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Iniciar sesión' })).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Crear cuenta' })).toBeOnTheScreen();

  request.mockReset();
  stubLotApi(request);
  await fireEvent.press(screen.getByRole('button', { name: 'Reintentar' }));
  expect(await screen.findByText('211')).toBeOnTheScreen();
  expect(screen.queryByRole('button', { name: 'Reintentar' })).toBeNull();
});

test('Welcome shows the count, status and rates from fixtures', async () => {
  await renderScreen(<WelcomeScreen />, { language: 'es-DO' });

  expect(await screen.findByText('211')).toBeOnTheScreen();
  expect(screen.getByText('Abierto')).toBeOnTheScreen();
  expect(screen.getByText('Parqueo Banreservas')).toBeOnTheScreen();
  expect(screen.getByText('Zona Colonial, Santo Domingo')).toBeOnTheScreen();
  expect(await screen.findByText('RD$50.00')).toBeOnTheScreen();
  expect(screen.getByText('Primera hora')).toBeOnTheScreen();
});

test('Crear cuenta is the outline button', async () => {
  await renderScreen(<WelcomeScreen />, { language: 'es-DO' });
  expect(screen.getByRole('button', { name: 'Crear cuenta' }).props.className).toContain('border-brand-navy');
});
