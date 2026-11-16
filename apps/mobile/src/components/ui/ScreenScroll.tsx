import type { ReactNode } from 'react';
import { ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { screenPadding, type ScreenEdge, type ScreenHeader } from '@/design/layout';

/**
 * Scrolling screen body, vertical only. The padding is the 16 dp gutter plus the safe-area insets (M2B-52).
 * `edge="tabBar"` is for screens above the tab bar, which already clears the home indicator.
 * `header="stack"` is for screens under a navigation header, which already clears the notch; headerless screens (the default)
 * add the top inset themselves.
 * `banner` is a full-bleed part at the very top (a `WavyHeader`): it scrolls with the content, bleeds through the gutter and
 * clears the notch itself.
 */
export function ScreenScroll({
  children,
  banner,
  edge = 'screen',
  header = 'none',
}: {
  children: ReactNode;
  banner?: ReactNode;
  edge?: ScreenEdge;
  header?: ScreenHeader;
}) {
  const insets = useSafeAreaInsets();
  const padding = screenPadding(insets, edge, banner ? 'banner' : header);
  return (
    <ScrollView
      testID="screen-scroll"
      // All padding is explicit; "automatic" would add the iOS top safe area again under a full-bleed banner.
      contentInsetAdjustmentBehavior="never"
      keyboardShouldPersistTaps="handled"
      className="flex-1 bg-surface"
      contentContainerStyle={{ ...padding, gap: 16 }}
    >
      {banner ? <View style={{ marginLeft: -padding.paddingLeft, marginRight: -padding.paddingRight }}>{banner}</View> : null}
      {children}
    </ScrollView>
  );
}
