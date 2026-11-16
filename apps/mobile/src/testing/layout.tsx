import type { QueryClient } from '@tanstack/react-query';
import { screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';
import type { ReactTestRendererNode } from 'react-test-renderer';
import { StyleSheet } from 'react-native';
import { SafeAreaProvider, type EdgeInsets } from 'react-native-safe-area-context';

import type { AppLanguage } from '@/lib/i18n';

import { renderScreen } from './render';

/** The two windows every screen is checked at: the narrowest supported phone and a large one (M2B-52). */
export const LAYOUT_SIZES = {
  small: { width: 360, height: 640 },
  large: { width: 430, height: 932 },
} as const;

/** A phone with a notch and a home indicator. */
export const DEVICE_INSETS: EdgeInsets = { top: 24, bottom: 34, left: 0, right: 0 };

/**
 * Renders a screen in a SafeAreaProvider whose frame has the given size and whose insets are fixed. Only the safe-area
 * frame carries the size: layout here is measured through padding, not pixels, so the two sizes prove the padding does not
 * depend on the window. The real overflow check is the device pass.
 */
export async function renderAtSize(
  ui: ReactElement,
  size: keyof typeof LAYOUT_SIZES,
  opts: { language?: AppLanguage; queryClient?: QueryClient; insets?: EdgeInsets } = {},
): Promise<void> {
  const { width, height } = LAYOUT_SIZES[size];
  const frame = { x: 0, y: 0, width, height };
  await renderScreen(
    <SafeAreaProvider initialMetrics={{ frame, insets: opts.insets ?? DEVICE_INSETS }}>{ui}</SafeAreaProvider>,
    { language: opts.language, queryClient: opts.queryClient },
  );
}

/** The scroll container's resolved padding, read from `screen-scroll`. */
export function scrollPadding(): { top: number; left: number; right: number; bottom: number } {
  const style = StyleSheet.flatten(screen.getByTestId('screen-scroll').props.contentContainerStyle) ?? {};
  return { top: style.paddingTop ?? 0, left: style.paddingLeft ?? 0, right: style.paddingRight ?? 0, bottom: style.paddingBottom ?? 0 };
}

/** Counts rendered nodes that scroll sideways; every screen must have none. */
export function countHorizontalScrolls(): number {
  const walk = (node: ReactTestRendererNode | null): number => {
    if (node === null || typeof node === 'string') return 0;
    const own = node.props.horizontal === true ? 1 : 0;
    return own + (node.children ?? []).reduce((sum, child) => sum + walk(child), 0);
  };
  const tree = screen.toJSON();
  return (Array.isArray(tree) ? tree : [tree]).reduce((sum, node) => sum + walk(node), 0);
}
