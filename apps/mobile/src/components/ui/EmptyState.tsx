import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from './AppText';

type Props = { title: string; body: string; action?: ReactNode };

/** An empty list that teaches the next step. The ghost plate is decoration. */
export function EmptyState({ title, body, action }: Props) {
  return (
    <View className="items-center gap-3 py-8">
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        className="h-14 w-36 overflow-hidden rounded-plate border-2 border-dashed border-control-line"
      >
        <View className="h-2 bg-hairline" />
      </View>
      <AppText weight="semibold" variant="title" className="text-center">
        {title}
      </AppText>
      <AppText variant="caption" className="text-center">
        {body}
      </AppText>
      {action ? <View className="mt-2 self-stretch">{action}</View> : null}
    </View>
  );
}
