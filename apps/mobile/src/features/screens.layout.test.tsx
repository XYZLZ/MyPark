/**
 * Padding contract for every screen that renders ScreenScroll: the 16 dp gutter, the top inset for headerless screens,
 * and the bottom inset left to the tab bar where there is one. These tests read the computed padding; they cannot see
 * overflow or clipping. The static guard (`npm run check:layout`) enforces "nothing scrolls sideways", and the device
 * pass (360x640 and a large phone at font scale 1.3, M2B-52) is the real overflow and clipping check.
 */
import { screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';

import { http } from '@/services/runtime';
import { countHorizontalScrolls, DEVICE_INSETS, LAYOUT_SIZES, renderAtSize, scrollPadding } from '@/testing/layout';

import { ForgotPasswordScreen } from './auth/ForgotPasswordScreen';
import { LegalDocumentScreen } from './auth/LegalDocumentScreen';
import { LoginScreen } from './auth/LoginScreen';
import { RegisterScreen } from './auth/RegisterScreen';
import { ResetPasswordScreen } from './auth/ResetPasswordScreen';
import { VerifyScreen } from './auth/VerifyScreen';
import { LegalGateScreen } from './legal/LegalGateScreen';
import { DeleteAccountScreen } from './profile/DeleteAccountScreen';
import { ProfileScreen } from './profile/ProfileScreen';
import { RestoreFailedScreen } from './session/RestoreFailedScreen';
import { VehicleFormScreen } from './vehicles/VehicleFormScreen';
import { VehiclesScreen } from './vehicles/VehiclesScreen';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn(), replace: jest.fn(), dismissTo: jest.fn() }) }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { logout: jest.fn(), signOutLocal: jest.fn(), applyTokens: jest.fn(), restore: jest.fn() },
}));
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

type Placement = {
  /** 'screen': nothing below it. 'tabBar': it sits above the tab bar. */
  edge: 'screen' | 'tabBar';
  /** 'none': the route hides the header. 'stack': a navigation header sits above it. */
  header: 'none' | 'stack';
};
const HEADERLESS: Placement = { edge: 'screen', header: 'none' }; // (auth), (gate) and restore-failed routes
const RECOVERY: Placement = { edge: 'screen', header: 'stack' }; // (recovery) routes have a header
const IN_TAB: Placement = { edge: 'tabBar', header: 'stack' }; // (tabs) stacks have a header and the tab bar

const SCREENS: [string, () => ReactElement, Placement][] = [
  ['LoginScreen', () => <LoginScreen />, HEADERLESS],
  ['RegisterScreen', () => <RegisterScreen />, HEADERLESS],
  ['VerifyScreen', () => <VerifyScreen />, HEADERLESS],
  ['LegalDocumentScreen', () => <LegalDocumentScreen kind="terms" />, HEADERLESS],
  ['LegalGateScreen', () => <LegalGateScreen />, HEADERLESS],
  ['RestoreFailedScreen', () => <RestoreFailedScreen />, HEADERLESS],
  ['ForgotPasswordScreen', () => <ForgotPasswordScreen />, RECOVERY],
  ['ResetPasswordScreen', () => <ResetPasswordScreen />, RECOVERY],
  ['VehiclesScreen', () => <VehiclesScreen />, IN_TAB],
  ['VehicleFormScreen', () => <VehicleFormScreen />, IN_TAB],
  ['ProfileScreen', () => <ProfileScreen />, IN_TAB],
  ['DeleteAccountScreen', () => <DeleteAccountScreen />, IN_TAB],
];

// Requests never settle: every screen is measured in its first (loading or form) state, with no async updates.
beforeEach(() => {
  jest.mocked(http.request).mockReset().mockReturnValue(new Promise(() => undefined));
});

describe.each(Object.keys(LAYOUT_SIZES) as (keyof typeof LAYOUT_SIZES)[])('at %s', (size) => {
  test.each(SCREENS)('%s pads the gutter and the safe areas', async (_name, make, { edge, header }) => {
    await renderAtSize(make(), size);
    expect(scrollPadding()).toEqual({
      top: header === 'none' ? DEVICE_INSETS.top + 16 : 24,
      left: 16,
      right: 16,
      bottom: edge === 'screen' ? 24 + DEVICE_INSETS.bottom : 24,
    });
    expect(countHorizontalScrolls()).toBe(0);
  });
});

test('side insets widen the gutter and a taller cutout lowers a headerless screen', async () => {
  await renderAtSize(<LoginScreen />, 'small', { insets: { top: 48, bottom: 34, left: 12, right: 12 } });
  expect(scrollPadding()).toMatchObject({ top: 64, left: 28, right: 28 });
  expect(screen.getByTestId('screen-scroll')).toBeTruthy();
});
