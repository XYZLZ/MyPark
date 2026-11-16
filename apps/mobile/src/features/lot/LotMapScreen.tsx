import { View } from 'react-native';

import { ScreenScroll } from '@/components/ui/ScreenScroll';
import type { ScreenEdge } from '@/design/layout';

import { FreshnessLine } from './components/FreshnessLine';
import { LotErrorPanel } from './components/LotErrorPanel';
import { MapLegend } from './components/MapLegend';
import { SpaceMap } from './components/SpaceMap';
import { SpacesSummary } from './components/SpacesSummary';
import { StatusPill } from './components/StatusPill';
import { mapCardStyle } from './map-layout';
import { useLotData } from './useLotData';

/**
 * The whole lot, 280 cells (M2B-49). It sits under a stack header, which owns the title and the way back to the lot screen.
 * Status and freshness only, no count (PF19). `edge` is 'tabBar' inside the tabs and 'screen' before login.
 */
export function LotMapScreen({ edge }: { edge: ScreenEdge }) {
  const { availability, spaces, connection, mapReady, loadError, retry, snapshotAt } = useLotData();

  return (
    <ScreenScroll edge={edge} header="stack">
      {loadError && !spaces ? (
        <LotErrorPanel onRetry={retry} />
      ) : (
        <View testID="map-card" style={mapCardStyle} className="gap-3 rounded-panel border border-hairline bg-surface">
          {availability ? (
            <View className="gap-3">
              <StatusPill status={availability.status} />
              <FreshnessLine
                connection={connection.status}
                updatedAt={availability.updated_at}
                lastFrameAt={connection.lastFrameAt}
                snapshotAt={snapshotAt}
              />
            </View>
          ) : null}
          <SpaceMap spaces={spaces ?? null} ready={mapReady} />
          <MapLegend />
          {spaces ? <SpacesSummary spaces={spaces} /> : null}
        </View>
      )}
    </ScreenScroll>
  );
}
