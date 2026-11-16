import type { Connection } from '@mypark/contracts';

// The store itself is shared with web and lives in contracts (PF16).
export { createConnectionStore, type Connection, type ConnectionStore } from '@mypark/contracts';

/** Before the first frame of a connection. A constant, so `useSyncExternalStore` sees stable snapshots. */
export const CONNECTING: Connection = Object.freeze({ status: 'connecting', lastFrameAt: null, spacesReady: false });
