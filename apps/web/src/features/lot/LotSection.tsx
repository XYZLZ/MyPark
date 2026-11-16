import type { ReactNode } from 'react';
import { readPublicApiOrigin } from '@/lib/env/public-api';
import { LotErrorPanel } from './components/LotErrorPanel';
import { LotLive } from './LotLive';
import { loadLotSnapshot } from './server';

/**
 * Loads the lot on the server and hands it to the live block, or renders the error panel when
 * the API is down (M2B-17). On the home it passes the actions through, so they show either way. The home puts this in a `Suspense`, so nothing here delays the hero.
 */
export async function LotSection({ variant, actions }: { variant: 'home' | 'compact'; actions?: ReactNode }) {
  const [snapshot, apiOrigin] = [await loadLotSnapshot(), readPublicApiOrigin()];
  if (!snapshot) {
    return (
      <>
        <div className="lg:col-start-2 lg:row-start-1">
          <LotErrorPanel />
        </div>
        {actions}
      </>
    );
  }
  return <LotLive variant={variant} lotId={snapshot.lot.id} apiOrigin={apiOrigin} snapshot={snapshot} actions={actions} />;
}
