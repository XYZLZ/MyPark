import { readPublicApiOrigin } from '@/lib/env/public-api';
import { LotErrorPanel } from './components/LotErrorPanel';
import { LotLive } from './LotLive';
import { loadLotSnapshot } from './server';

/**
 * Loads the lot on the server and hands it to the live block, or renders the error panel when
 * the API is down (M2B-17). The home puts this in a `Suspense`, so nothing here delays the hero.
 */
export async function LotSection({ variant }: { variant: 'home' | 'compact' }) {
  const [snapshot, apiOrigin] = [await loadLotSnapshot(), readPublicApiOrigin()];
  if (!snapshot) return <LotErrorPanel />;
  return <LotLive variant={variant} lotId={snapshot.lot.id} apiOrigin={apiOrigin} snapshot={snapshot} />;
}
