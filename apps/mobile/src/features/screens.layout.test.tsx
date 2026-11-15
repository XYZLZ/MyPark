import { screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';

import { http } from '@/services/runtime';
import { countHorizontalScrolls, DEVICE_INSETS, LAYOUT_SIZES, renderAtSize, scrollPadding } from '@/testing/layout';

import { ForgotPasswordScreen } from './auth/ForgotPasswordScreen';
import { LoginScreen } from './auth/LoginScreen';
import { RegisterScreen } from './auth/RegisterScreen';
import { ResetPasswordScreen } from './auth/ResetPasswordScreen';
import { VerifyScreen } from './auth/VerifyScreen';
import { ProfileScreen } from './profile/ProfileScreen';
import { VehiclesScreen } from './vehicles/VehiclesScreen';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn(), replace: jest.fn() }) }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { logout: jest.fn(), signOutLocal: jest.fn(), applyTokens: jest.fn() },
}));
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

const SCREENS: [string, () => ReactElement, 'screen' | 'tabBar'][] = [
  ['LoginScreen', () => <LoginScreen />, 'screen'],
  ['RegisterScreen', () => <RegisterScreen />, 'screen'],
  ['VerifyScreen', () => <VerifyScreen />, 'screen'],
  ['ForgotPasswordScreen', () => <ForgotPasswordScreen />, 'screen'],
  ['ResetPasswordScreen', () => <ResetPasswordScreen />, 'screen'],
  ['VehiclesScreen', () => <VehiclesScreen />, 'tabBar'],
  ['ProfileScreen', () => <ProfileScreen />, 'tabBar'],
];

// Requests never settle: every screen is measured in its first (loading or form) state, with no async updates.
beforeEach(() => {
  jest.mocked(http.request).mockReset().mockReturnValue(new Promise(() => undefined));
});

describe.each(['small', 'large'] as const)('at %s', (size) => {
  test.each(SCREENS)('%s keeps the 16 dp gutter and never scrolls sideways', async (_name, make, edge) => {
    await renderAtSize(make(), size);
    const padding = scrollPadding();
    expect(padding.left).toBe(16);
    expect(padding.right).toBe(16);
    // A tab screen leaves the home indicator to the tab bar.
    expect(padding.bottom).toBe(edge === 'screen' ? 24 + DEVICE_INSETS.bottom : 24);
    expect(countHorizontalScrolls()).toBe(0);
    expect(LAYOUT_SIZES[size].width).toBeGreaterThanOrEqual(360);
  });
});

test('side insets widen the gutter', async () => {
  await renderAtSize(<LoginScreen />, 'small', { insets: { top: 24, bottom: 34, left: 12, right: 12 } });
  expect(scrollPadding()).toMatchObject({ left: 28, right: 28 });
  expect(screen.getByTestId('screen-scroll')).toBeTruthy();
});
