import type { LotStatus } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

// Full class names, never built from the status: Uniwind only keeps what it can read.
const PILLS = {
  open: { tint: 'bg-status-open-tint', ink: 'text-status-open', mark: 'h-2.5 w-2.5 rounded-full' },
  full: { tint: 'bg-status-full-tint', ink: 'text-status-full', mark: 'h-2.5 w-2.5 rounded-sm' },
  closed: { tint: 'bg-status-closed-tint', ink: 'text-status-closed', mark: 'h-1 w-3 rounded-sm' },
} as const;
const MARK_BG = { open: 'bg-status-open', full: 'bg-status-full', closed: 'bg-status-closed' } as const;

/** Mark and text on the status tint: a circle for open, a square for full, a bar for closed (never color alone). */
export function StatusPill({ status }: { status: LotStatus }) {
  const { t } = useTranslation();
  const { tint, ink, mark } = PILLS[status];
  return (
    <View className={`h-8 flex-row items-center gap-2 self-start rounded-full px-3 ${tint}`}>
      <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className={`${mark} ${MARK_BG[status]}`} />
      <AppText weight="bold" variant="caption" className={ink}>
        {t(`availability.status.${status}`)}
      </AppText>
    </View>
  );
}
