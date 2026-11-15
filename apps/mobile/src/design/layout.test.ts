import { LAYOUT_MIN_WIDTH, MAX_FIXED_WIDTH, SCREEN_GUTTER, screenPadding, tabBarStyle } from './layout';

const insets = { top: 0, bottom: 34, left: 0, right: 0 };

test('the gutter constants agree', () => {
  expect(SCREEN_GUTTER).toBe(16);
  expect(MAX_FIXED_WIDTH).toBe(LAYOUT_MIN_WIDTH - 2 * SCREEN_GUTTER);
});

test('screenPadding adds side insets to the gutter, never replaces it', () => {
  expect(screenPadding({ ...insets, left: 8 }, 'screen')).toMatchObject({ paddingLeft: 24, paddingRight: 16, paddingBottom: 58 });
});

test('a tab screen leaves the bottom inset to the tab bar', () => {
  expect(screenPadding(insets, 'tabBar').paddingBottom).toBe(24);
});

test('a headerless screen clears the notch: top inset plus the gutter, never below 24', () => {
  expect(screenPadding({ ...insets, top: 48 }, 'screen', 'none').paddingTop).toBe(64);
  expect(screenPadding({ ...insets, top: 24 }, 'screen', 'none').paddingTop).toBe(40);
  expect(screenPadding({ ...insets, top: 0 }, 'screen', 'none').paddingTop).toBe(24);
  expect(screenPadding({ ...insets, top: 4 }, 'screen', 'none').paddingTop).toBe(24);
});

test('a screen under a stack header keeps 24: the header already clears the top', () => {
  expect(screenPadding({ ...insets, top: 48 }, 'tabBar', 'stack').paddingTop).toBe(24);
});

test('tabBarStyle pads the home indicator', () => {
  expect(tabBarStyle(insets)).toMatchObject({ height: 98, paddingBottom: 34, paddingLeft: 16, paddingRight: 16 });
  expect(tabBarStyle({ ...insets, left: 12, right: 4 })).toMatchObject({ paddingLeft: 28, paddingRight: 20 });
});
