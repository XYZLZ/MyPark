import type { Connection } from '@mypark/contracts';

// The store and the registry are shared with mobile and live in contracts (PF16). `CONNECTING` is
// the store's initial value, so the server snapshot and the first client snapshot are one object.
export { CONNECTING, createConnectionStore, type Connection, type ConnectionStore } from '@mypark/contracts';

/** No usable `API_PUBLIC_URL`: the snapshot is shown and no stream is opened (M2B-16). */
export const UNAVAILABLE: Connection = Object.freeze({ status: 'unavailable', lastFrameAt: null, spacesReady: false });
