import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

/**
 * The brand banner: the lot's place and, signed out, a line about what the app shows. The photo is a placeholder.
 * It needs nothing from the API, so it renders at once.
 */
export function HeroPlaceholder({ tagline = false }: { tagline?: boolean }) {
  const { t } = useTranslation();
  return (
    // TODO(brand): replace the plain navy block with the official photo; it stays hidden from screen readers until then.
    <View className="gap-2 overflow-hidden rounded-panel bg-brand-navy-deep px-4 pb-16 pt-5">
      <View className="flex-row items-center gap-2">
        <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className="h-3 w-3 rounded-full border-2 border-brand-orange" />
        <AppText weight="semibold" className="flex-1 text-on-navy-muted">
          {t('home.location')}
        </AppText>
      </View>
      {tagline ? <AppText className="text-on-navy-muted">{t('lot.hero.subtitle')}</AppText> : null}
      <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className="mt-1 h-1 w-20 rounded-sm bg-brand-orange" />
    </View>
  );
}
