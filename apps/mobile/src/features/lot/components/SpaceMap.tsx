import type { SpaceState } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';
import { useSafeAreaFrame, useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from '@/components/ui/AppText';

import { cellSize, MAP_GAP, mapGridWidth } from '../map-layout';
import { cellHeight, SpaceCell } from './SpaceCell';

const BAY_COUNT = 280;

/**
 * The lot as a grid of shapes in code order, ten to a row (design §9.3, PF6). The grid is hidden from assistive tech: 280 cells
 * would be 280 focus stops, and `SpacesSummary` says the same in text. `limit` draws only the first spaces (the preview).
 * `ready` is false between a reconnect and its first full frame: the map is then dimmed and says it is updating.
 */
export function SpaceMap({ spaces, ready, limit }: { spaces: SpaceState[] | null; ready: boolean; limit?: number }) {
  const { t } = useTranslation();
  const frame = useSafeAreaFrame();
  const insets = useSafeAreaInsets();
  const size = cellSize(frame.width - insets.left - insets.right);
  const count = limit ?? BAY_COUNT;
  const dimmed = spaces !== null && !ready;

  return (
    <View>
      <View
        testID="space-grid"
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={{ width: mapGridWidth(size), flexDirection: 'row', flexWrap: 'wrap', gap: MAP_GAP, alignSelf: 'center' }}
        className={dimmed ? 'opacity-60' : ''}
      >
        {spaces
          ? spaces.slice(0, count).map((space) => <SpaceCell key={space.code} code={space.code} state={space.state} size={size} />)
          : Array.from({ length: count }, (_, i) => (
              <View key={i} testID="space-skeleton" style={{ width: size, height: cellHeight(size) }} className="rounded-[3px] bg-hairline" />
            ))}
      </View>
      {dimmed ? (
        <View pointerEvents="none" className="absolute inset-x-0 top-2 items-center">
          <AppText weight="semibold" variant="caption" className="rounded-control bg-surface px-3 py-1 text-ink">
            {t('spaces.updating')}
          </AppText>
        </View>
      ) : null}
    </View>
  );
}
