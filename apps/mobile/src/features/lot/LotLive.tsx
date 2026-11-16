import { availabilityFigures } from '@mypark/contracts';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { CountCard } from './components/CountCard';
import { FreshnessLine } from './components/FreshnessLine';
import { LotErrorPanel } from './components/LotErrorPanel';
import { LotInfo } from './components/LotInfo';
import { LotSkeleton } from './components/LotSkeleton';
import { RatesList } from './components/RatesList';
import { useAnnouncer } from './useAnnouncer';
import { useLotData } from './useLotData';

/**
 * The live block (design §4.3). Order: count card, the caller's actions right under it, the map slot (Task 12), then the lot
 * info and the rates. The figures come only from availability, never from the map (M2B-15). Without figures it shows a
 * skeleton while connecting and the error panel when nothing can be loaded; the actions stay on screen either way.
 */
export function LotLive({ actions }: { actions?: ReactNode }) {
  const { lot, availability, tariff, connection, loadError, retry, snapshotAt } = useLotData();
  const figures = availability ? availabilityFigures(availability) : null;
  useAnnouncer(
    availability ? { available: availability.available, status: availability.status, connection: connection.status } : null,
    { total: figures?.effectiveCapacity ?? 0, lastUpdateAt: connection.lastFrameAt ?? snapshotAt },
  );

  if (!availability || !figures) {
    return (
      <>
        {loadError ? <LotErrorPanel onRetry={retry} /> : <LotSkeleton />}
        {actions}
      </>
    );
  }

  return (
    <>
      <View className="-mt-14 gap-3 rounded-panel border border-hairline bg-surface p-4">
        <CountCard figures={figures} status={availability.status} />
        <View className="border-t border-hairline pt-3">
          <FreshnessLine
            connection={connection.status}
            updatedAt={availability.updated_at}
            lastFrameAt={connection.lastFrameAt}
            snapshotAt={snapshotAt}
          />
        </View>
      </View>
      {actions}
      {/* The map preview slot (Task 12). */}
      {lot ? <LotInfo lot={lot} /> : null}
      {tariff ? <RatesList tariff={tariff} /> : null}
    </>
  );
}
