import type { ReactNode } from 'react';
import { ScrollView } from 'react-native';

/** Scrolling screen body. The system inset handling keeps content clear of headers and tab bars. */
export function ScreenScroll({ children }: { children: ReactNode }) {
  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps="handled"
      className="flex-1 bg-surface"
      contentContainerClassName="gap-4 px-4 py-6"
    >
      {children}
    </ScrollView>
  );
}
