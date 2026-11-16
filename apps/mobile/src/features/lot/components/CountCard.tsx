import type { AvailabilityFigures, LotStatus } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

import { StatusPill } from './StatusPill';

/**
 * The compact count (M2B-24): the available number, "of {capacity}", the out of service line and the status.
 * A closed lot says so instead of a number. The group is one screen-reader element that says the count once; the
 * freshness line is read separately, by whoever renders it.
 */
export function CountCard({ figures, status }: { figures: AvailabilityFigures; status: LotStatus }) {
  const { t } = useTranslation();
  const closed = status === 'closed';
  const capacity = t('availability.of', { total: figures.effectiveCapacity });
  const outOfService = t('availability.outOfService', { count: figures.outOfService });
  const detail = figures.outOfService > 0 ? t('home.detailOos', { capacity, outOfService }) : capacity;
  const statusText = t(`availability.status.${status}`);
  const counts = { count: figures.available, total: figures.effectiveCapacity, status: statusText };
  const label = closed
    ? t('home.cardLabelClosed', { status: statusText, body: t('lot.unavailable.closedBody') })
    : figures.outOfService > 0
      ? t('home.cardLabelOos', { ...counts, outOfService })
      : t('home.cardLabel', counts);

  return (
    <View accessible accessibilityLabel={label} className="flex-row items-start gap-3">
      <CarDisc />
      <View className="flex-1">
        <View className="flex-row items-start justify-between gap-2">
          <AppText weight="semibold" className="flex-1 text-brand-navy">
            {t('availability.availableLong')}
          </AppText>
          <StatusPill status={status} />
        </View>
        {closed ? (
          <AppText weight="bold" maxFontSizeMultiplier={1.5} className="py-1 text-4xl text-status-closed">
            {statusText}
          </AppText>
        ) : (
          <AppText weight="bold" maxFontSizeMultiplier={1.5} className="text-[64px] leading-[72px] text-brand-navy">
            {figures.available}
          </AppText>
        )}
        <AppText variant="caption">{closed ? t('lot.unavailable.closedBody') : detail}</AppText>
      </View>
    </View>
  );
}

/** A car drawn with Views on a sky disc, so the app needs no icon package. Decoration. */
function CarDisc() {
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="h-14 w-14 items-center justify-center rounded-full bg-brand-sky"
    >
      <View className="h-2.5 w-5 rounded-t-md bg-brand-navy" />
      <View className="h-3.5 w-8 rounded-md bg-brand-navy" />
      <View className="-mt-1 w-8 flex-row justify-between px-1">
        <View className="h-2 w-2 rounded-full border border-brand-sky bg-brand-navy" />
        <View className="h-2 w-2 rounded-full border border-brand-sky bg-brand-navy" />
      </View>
    </View>
  );
}
