import type { ViewStyle } from 'react-native';
import type { EdgeInsets } from 'react-native-safe-area-context';

/** The one screen gutter, left and right (M2B-52). Full-bleed parts bleed, their content sits inside it. */
export const SCREEN_GUTTER = 16;
/** The narrowest window the layout is built and checked for. */
export const LAYOUT_MIN_WIDTH = 360;
/** The widest fixed width that still fits inside the gutters at the narrowest window. */
export const MAX_FIXED_WIDTH = LAYOUT_MIN_WIDTH - 2 * SCREEN_GUTTER;

/** Content padding below the last item of a scroll view, before any bottom inset. */
const SCREEN_BOTTOM = 24;
/** The tab bar's own height, above the home-indicator inset. */
const TAB_BAR_HEIGHT = 64;

export type ScreenEdge = 'screen' | 'tabBar';
/** 'stack': a navigation header sits above and already clears the top inset. 'none': the screen is headerless and clears it itself. */
export type ScreenHeader = 'none' | 'stack';

/** Content padding at the top of a scroll view. */
const SCREEN_TOP = 24;

/**
 * Horizontal padding is the gutter plus the side insets (landscape, foldables), never a replacement for it.
 * A screen above the tab bar leaves the bottom inset to the bar; any other screen adds it to its last item.
 * A headerless screen (auth, gate, restore-failed) starts below the notch: `max(24, top inset + 16)`.
 * A screen under a stack header keeps 24.
 */
export function screenPadding(
  insets: EdgeInsets,
  edge: ScreenEdge,
  header: ScreenHeader = 'none',
): { paddingTop: number; paddingLeft: number; paddingRight: number; paddingBottom: number } {
  return {
    paddingTop: header === 'none' ? Math.max(SCREEN_TOP, insets.top + SCREEN_GUTTER) : SCREEN_TOP,
    paddingLeft: SCREEN_GUTTER + insets.left,
    paddingRight: SCREEN_GUTTER + insets.right,
    paddingBottom: SCREEN_BOTTOM + (edge === 'screen' ? insets.bottom : 0),
  };
}

/** The bar bleeds to the edges, grows by the home-indicator inset, and keeps its content inside the gutter. */
export function tabBarStyle(insets: EdgeInsets): ViewStyle {
  return {
    height: TAB_BAR_HEIGHT + insets.bottom,
    paddingBottom: insets.bottom,
    paddingLeft: SCREEN_GUTTER + insets.left,
    paddingRight: SCREEN_GUTTER + insets.right,
  };
}
