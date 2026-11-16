import type { ConnectionStatus } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { ActivityIndicator, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { tokens } from '@/design/tokens';
import { formatClock } from '@/lib/format/time';
import type { AppLanguage } from '@/lib/i18n';

type Props = {
  connection: ConnectionStatus;
  /** `availability.updated_at`: the last count change (M2B-14). */
  updatedAt: string;
  /** Client time of the last frame; `null` before the first one. */
  lastFrameAt: number | null;
  /** Client time of the REST snapshot. */
  snapshotAt: number;
};

/**
 * Two times, on purpose (M2B-14). Live says when the count last changed; stale says how old the values on screen are, which
 * only the last frame time answers. Text and a mark, never color alone. `formatClock` gives `''` for a bad time, so the time
 * is left out instead of dangling.
 */
export function FreshnessLine({ connection, updatedAt, lastFrameAt, snapshotAt }: Props) {
  const { t, i18n } = useTranslation();
  const language = i18n.language as AppLanguage;

  if (connection === 'stale') {
    const time = formatClock(lastFrameAt ?? snapshotAt, language);
    return (
      <View className="gap-1 rounded-control bg-status-stale-tint px-3 py-2">
        <View className="flex-row items-center gap-2">
          <ClockMark />
          <AppText weight="semibold" variant="caption" className="flex-1 text-status-stale">
            {t('availability.stale', { time }).trim()}
          </AppText>
        </View>
        <View className="flex-row items-center gap-2">
          <ActivityIndicator size="small" color={tokens.statusStale} />
          <AppText weight="semibold" variant="caption" className="text-status-stale">
            {t('availability.reconnecting')}
          </AppText>
        </View>
      </View>
    );
  }

  if (connection === 'unavailable') {
    const time = formatClock(snapshotAt, language);
    return (
      <View className="rounded-control bg-status-closed-tint px-3 py-2">
        <AppText weight="semibold" variant="caption" className="text-status-closed">
          {time === '' ? t('availability.unavailable') : t('availability.unavailableAt', { time })}
        </AppText>
      </View>
    );
  }

  if (connection === 'connecting') {
    return <AppText variant="caption">{t('availability.connecting')}</AppText>;
  }

  const lastChange = formatClock(updatedAt, language);
  return (
    <View className="flex-row items-center gap-2">
      <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className="h-2.5 w-2.5 rounded-full bg-status-open" />
      <AppText variant="caption" className="flex-1">
        <AppText weight="bold" variant="caption" className="text-status-open">
          {t('availability.live')}
        </AppText>
        {lastChange === '' ? null : ` · ${t('availability.lastChange', { time: lastChange })}`}
      </AppText>
    </View>
  );
}

/** A 16 dp clock drawn with Views, for the stale line. */
function ClockMark() {
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="h-4 w-4 items-center justify-center rounded-full border-2 border-status-stale"
    >
      <View className="absolute top-0.5 h-1 w-0.5 bg-status-stale" />
    </View>
  );
}
