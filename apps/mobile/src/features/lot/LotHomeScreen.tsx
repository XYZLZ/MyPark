import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { ScreenScroll } from '@/components/ui/ScreenScroll';

import { LotHeader } from './components/LotHeader';
import { LotLive } from './LotLive';

/** The Inicio tab: the live lot for a signed-in driver. */
export function LotHomeScreen() {
  const { t } = useTranslation();
  return (
    <ScreenScroll
      edge="tabBar"
      banner={
        <LotHeader
          trailing={
            <View
              accessibilityRole="image"
              accessibilityLabel={t('nav.profile')}
              className="h-12 w-12 items-center justify-center rounded-full bg-white/15"
            >
              <View className="h-2.5 w-2.5 rounded-full bg-brand-primary-contrast" />
              <View className="mt-0.5 h-2.5 w-5 rounded-t-full bg-brand-primary-contrast" />
            </View>
          }
        />
      }
    >
      <LotLive />
    </ScreenScroll>
  );
}
