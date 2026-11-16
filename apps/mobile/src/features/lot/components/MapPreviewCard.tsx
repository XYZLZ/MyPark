import type { SpaceState } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';

import { mapCardStyle, PREVIEW_COUNT } from '../map-layout';
import { MapLegend } from './MapLegend';
import { SpaceMap } from './SpaceMap';
import { SpacesSummary } from './SpacesSummary';

/**
 * The first 40 spaces with the legend, the sensor summary and a way to the full map (M2B-49). The summary covers all
 * the spaces, not just the 40 drawn. The skeleton shows while the spaces load.
 */
export function MapPreviewCard({
  spaces,
  ready,
  mapHref,
}: {
  spaces: SpaceState[] | null;
  ready: boolean;
  mapHref: '/map' | '/home/map';
}) {
  const { t } = useTranslation();
  const router = useRouter();
  return (
    <View style={mapCardStyle} className="gap-3 rounded-panel border border-hairline bg-surface">
      <AppText accessibilityRole="header" weight="bold" variant="title" className="text-brand-navy">
        {t('spaces.title')}
      </AppText>
      <SpaceMap spaces={spaces} ready={ready} limit={PREVIEW_COUNT} />
      {spaces ? (
        <AppText variant="caption" className="text-center">
          {t('spaces.previewCount', { shown: Math.min(PREVIEW_COUNT, spaces.length), total: spaces.length })}
        </AppText>
      ) : null}
      <MapLegend />
      {spaces ? <SpacesSummary spaces={spaces} /> : null}
      <Button variant="outline" label={t('spaces.viewFull')} onPress={() => router.push(mapHref)} />
    </View>
  );
}
