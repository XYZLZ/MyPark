import { View } from 'react-native';

import { AppText } from './AppText';

const SIZE_CLASS = {
  sm: 'size-8 rounded-lg',
  md: 'size-14 rounded-xl',
  lg: 'size-24 rounded-2xl',
} as const;

const LETTER_CLASS = {
  sm: 'text-lg',
  md: 'text-3xl',
  lg: 'text-6xl',
} as const;

export type ParkingSignSize = keyof typeof SIZE_CLASS;

/** The "P" mark. Decorative: hidden from screen readers. */
export function ParkingSign({ size = 'md' }: { size?: ParkingSignSize }) {
  return (
    <View
      accessible={false}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className={`items-center justify-center bg-brand-primary ${SIZE_CLASS[size]}`}
    >
      <AppText weight="bold" maxFontSizeMultiplier={1} className={`text-brand-primary-contrast ${LETTER_CLASS[size]}`}>
        P
      </AppText>
    </View>
  );
}
