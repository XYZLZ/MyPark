/**
 * How the BFF says "logout on all devices was not confirmed" (shared by the handler and the client).
 *
 * The cookies are cleared on every path, so this device is always signed out. Only a 2xx from Go
 * proves the other sessions were revoked. When Go was not reached, the proxy left no access cookie,
 * or Go answered anything else (a 401 included: this session is gone, the others may not be),
 * the BFF answers this status with an error envelope carrying this code. The client then lands
 * home with the `logout_all_failed` notice instead of claiming success.
 */
export const LOGOUT_ALL_UNCONFIRMED = 'session.logout_unconfirmed';
export const LOGOUT_ALL_UNCONFIRMED_STATUS = 502;

/** The `?notice=` value on the home page after an unconfirmed logout on all devices. */
export const LOGOUT_ALL_FAILED_NOTICE = 'logout_all_failed';
