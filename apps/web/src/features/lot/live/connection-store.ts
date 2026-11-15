import type { Connection } from '@mypark/contracts';

// The store itself is shared with mobile and lives in contracts (PF16).
export { createConnectionStore, type Connection, type ConnectionStore } from '@mypark/contracts';

/** Before the first frame of the first connection. Constants, so `useSyncExternalStore` sees stable snapshots. */
export const CONNECTING: Connection = Object.freeze({ status: 'connecting', lastFrameAt: null, spacesReady: false });

/** No usable `API_PUBLIC_URL`: the snapshot is shown and no stream is opened (M2B-16). */
export const UNAVAILABLE: Connection = Object.freeze({ status: 'unavailable', lastFrameAt: null, spacesReady: false });
