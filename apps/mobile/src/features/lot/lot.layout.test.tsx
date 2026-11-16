/**
 * Padding contract for the two lot screens (M2B-52): the 16 dp gutter, the bottom inset left to the tab bar on Inicio,
 * nothing scrolling sideways, and the header clearing the notch. These read computed padding; the device pass is the
 * real overflow and clipping check.
 */
import { screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { WelcomeScreen } from '@/features/auth/WelcomeScreen';
import { http } from '@/services/runtime';
import { countHorizontalScrolls, DEVICE_INSETS, LAYOUT_SIZES, renderAtSize, scrollPadding } from '@/testing/layout';
import { stubLotApi } from '@/testing/lot-api';

import { LotHomeScreen } from './LotHomeScreen';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }), Redirect: () => null }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));
jest.mock('@/features/lot/live/useLiveLot', () => ({
  useLiveLot: () => ({ status: 'live', lastFrameAt: 0, spacesReady: true }),
}));

beforeEach(() => {
  jest.mocked(http.request).mockReset();
  stubLotApi(jest.mocked(http.request));
});

const sizes = Object.keys(LAYOUT_SIZES) as (keyof typeof LAYOUT_SIZES)[];

test.each(sizes)('Welcome and Inicio at %s keep the 16 dp gutter and never scroll sideways', async (size) => {
  await renderAtSize(<WelcomeScreen />, size, { language: 'es-DO' });
  await screen.findByText('211');
  expect(scrollPadding()).toMatchObject({ left: 16, right: 16, bottom: 24 + DEVICE_INSETS.bottom });
  expect(countHorizontalScrolls()).toBe(0);

  await screen.unmount();
  await renderAtSize(<LotHomeScreen />, size, { language: 'es-DO' });
  await screen.findByText('211');
  expect(scrollPadding()).toMatchObject({ left: 16, right: 16, bottom: 24 }); // the tab bar owns the inset
  expect(countHorizontalScrolls()).toBe(0);
});

test('the scroll adds no inset of its own: all padding is explicit (no iOS double top inset)', async () => {
  await renderAtSize(<LotHomeScreen />, 'small', { language: 'es-DO' });
  expect(screen.getByTestId('screen-scroll').props.contentInsetAdjustmentBehavior).toBe('never');
});

test('Inicio shows the hero with the place and no tagline', async () => {
  await renderAtSize(<LotHomeScreen />, 'small', { language: 'es-DO' });
  expect(screen.getByText('Zona Colonial, Santo Domingo')).toBeOnTheScreen();
  expect(screen.queryByText('Mira cuántos espacios hay libres antes de llegar.')).toBeNull();
});

const headerPadding = () => {
  const style = StyleSheet.flatten(screen.getByTestId('wavy-header').props.style) ?? {};
  return { top: style.paddingTop, left: style.paddingLeft, right: style.paddingRight };
};

test('the header adds the top inset', async () => {
  await renderAtSize(<LotHomeScreen />, 'small', { language: 'es-DO' });
  expect(headerPadding().top).toBe(16 + 24);
});

test('the header keeps the gutter and adds the side insets, and the content below starts under it', async () => {
  await renderAtSize(<LotHomeScreen />, 'large', { language: 'es-DO', insets: { top: 0, bottom: 0, left: 8, right: 12 } });
  expect(headerPadding()).toEqual({ top: 16, left: 24, right: 28 });
  expect(scrollPadding()).toMatchObject({ top: 0, left: 24, right: 28 });
});
