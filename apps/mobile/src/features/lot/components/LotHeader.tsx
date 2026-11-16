import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { ParkingSign } from '@/components/ui/ParkingSign';
import { WavyHeader } from '@/components/ui/WavyHeader';

/**
 * The navy header of the two lot screens: the logo, the lot name and one control (the language switch signed out,
 * the profile avatar signed in). The row wraps, so a large font or a narrow phone moves the control down instead of clipping it.
 */
export function LotHeader({ trailing }: { trailing: ReactNode }) {
  const { t } = useTranslation();
  return (
    <WavyHeader>
      <View className="flex-row flex-wrap items-center justify-between gap-3">
        <View className="flex-row items-center gap-3">
          {/* TODO(brand): replace the placeholder sign with the official logo. */}
          <ParkingSign size="sm" />
          <AppText accessibilityRole="header" weight="bold" variant="title" className="text-brand-primary-contrast">
            {t('lot.title')}
          </AppText>
        </View>
        {trailing}
      </View>
    </WavyHeader>
  );
}
