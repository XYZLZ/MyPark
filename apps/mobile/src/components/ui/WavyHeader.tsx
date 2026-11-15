import type { ReactNode } from 'react';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { SCREEN_GUTTER } from '@/design/layout';

/**
 * Full-bleed navy header. Its top padding clears the notch and its content stays inside the gutter plus the side insets.
 * The wave is a wide rounded View that cuts into the navy, so no SVG is needed.
 */
export function WavyHeader({ children }: { children: ReactNode }) {
  const insets = useSafeAreaInsets();
  return (
    <View
      className="overflow-hidden bg-brand-navy pb-10"
      style={{ paddingTop: SCREEN_GUTTER + insets.top, paddingLeft: SCREEN_GUTTER + insets.left, paddingRight: SCREEN_GUTTER + insets.right }}
    >
      {children}
      <View
        pointerEvents="none"
        importantForAccessibility="no-hide-descendants"
        accessibilityElementsHidden
        className="absolute -bottom-6 -left-8 -right-8 h-12 rounded-[100px] bg-surface"
      />
    </View>
  );
}
