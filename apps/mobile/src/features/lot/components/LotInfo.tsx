import type { Lot } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { Linking, Pressable, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

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
        <Pressable
          accessibilityRole="link"
          accessibilityLabel={t('lot.openInMaps')}
          accessibilityHint={t('home.openInMapsHint')}
          onPress={() => void Linking.openURL(lot.map_url).catch(() => {})}
          className="min-h-12 justify-center self-start focus:border-2 focus:border-brand-navy"
        >
          <AppText weight="semibold" className="text-brand-primary underline">
            {t('lot.openInMaps')}
          </AppText>
        </Pressable>
      ) : null}
    </View>
  );
}
