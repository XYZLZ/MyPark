import { useEffect, type ReactNode } from 'react';
import { AccessibilityInfo, Platform, View } from 'react-native';

import { AppText } from './AppText';

const TONE_CLASS = {
  error: 'border-danger bg-danger-tint',
  info: 'border-brand-primary bg-brand-primary-tint',
} as const;

const TEXT_CLASS = {
  error: 'text-danger',
  info: 'text-ink',
} as const;

export type BannerTone = keyof typeof TONE_CLASS;

type Props = { tone: BannerTone; children: ReactNode };

/** Errors are alerts; everything else is a polite live region. Never color alone: the text says what happened. */
export function Banner({ tone, children }: Props) {
  // Android reads live regions; iOS needs an explicit announcement.
  useEffect(() => {
    if (Platform.OS === 'ios' && typeof children === 'string') AccessibilityInfo.announceForAccessibility(children);
  }, [children]);

  return (
    <View
      testID="banner"
      accessible
      accessibilityRole={tone === 'error' ? 'alert' : undefined}
      accessibilityLiveRegion="polite"
      className={`rounded-control border px-4 py-3 ${TONE_CLASS[tone]}`}
    >
      <AppText weight="medium" className={TEXT_CLASS[tone]}>
        {children}
      </AppText>
    </View>
  );
}
