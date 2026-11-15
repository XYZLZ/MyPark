import type { ReactNode } from 'react';
import { ScrollView } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { screenPadding, type ScreenEdge, type ScreenHeader } from '@/design/layout';

/**
 * Scrolling screen body, vertical only. The padding is the 16 dp gutter plus the safe-area insets (M2B-52).
 * `edge="tabBar"` is for screens above the tab bar, which already clears the home indicator.
 * `header="stack"` is for screens under a navigation header, which already clears the notch; headerless screens (the default)
 * add the top inset themselves.
 */
export function ScreenScroll({
  children,
  edge = 'screen',
  header = 'none',
}: {
  children: ReactNode;
  edge?: ScreenEdge;
  header?: ScreenHeader;
}) {
  const insets = useSafeAreaInsets();
  return (
    <ScrollView
      testID="screen-scroll"
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps="handled"
      className="flex-1 bg-surface"
      contentContainerStyle={{ ...screenPadding(insets, edge, header), gap: 16 }}
    >
      {children}
    </ScrollView>
  );
}
