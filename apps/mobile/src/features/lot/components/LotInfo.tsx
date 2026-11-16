import type { Lot } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { Linking, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';

/** `map_url` comes from the API; only an absolute http(s) URL becomes a link. */
const isWebUrl = (value: string) => /^https?:\/\//i.test(value);

/** Address, hours and the Maps link (M2B-25). The link opens the maps app and says so in its hint. */
export function LotInfo({ lot }: { lot: Lot }) {
  const { t } = useTranslation();
  return (
    <View className="gap-3 rounded-panel border border-hairline bg-surface p-4">
      <AppText accessibilityRole="header" weight="bold" variant="title" className="text-brand-navy">
        {t('lot.info')}
      </AppText>
      <View>
        <AppText weight="bold" variant="caption" className="text-brand-navy">
          {t('lot.address')}
        </AppText>
        <AppText>{lot.address}</AppText>
      </View>
      <View>
        <AppText weight="bold" variant="caption" className="text-brand-navy">
          {t('lot.hours.label')}
        </AppText>
        <AppText>{lot.opening_hours.always_open ? t('lot.hours.alwaysOpen') : t('lot.hours.varies')}</AppText>
      </View>
      {isWebUrl(lot.map_url) ? (
        <Button
          variant="outline"
          role="link"
          label={t('lot.openInMaps')}
          hint={t('home.openInMapsHint')}
          icon={<PinMark />}
          onPress={() => void Linking.openURL(lot.map_url).catch(() => {})}
        />
      ) : null}
    </View>
  );
}

/** A small map pin drawn with Views. Decoration. */
function PinMark() {
  return (
    <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className="h-5 w-5 items-center justify-center rounded-full border-2 border-brand-navy">
      <View className="h-1.5 w-1.5 rounded-full bg-brand-navy" />
    </View>
  );
}
