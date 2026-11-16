// The store and the registry are shared with web and live in contracts (PF16). `CONNECTING` is the
// store's initial value, so the hook's fallback snapshot and the store's first one are one object.
export { CONNECTING, createConnectionStore, type Connection, type ConnectionStore } from '@mypark/contracts';
