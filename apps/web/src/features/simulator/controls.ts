import { ApiError, type SimulatorStatus } from '@mypark/contracts';
import type { StatusResult } from './api';

export type ConsoleMode = 'loading' | 'notConfigured' | 'forbidden' | 'unreachable' | 'error' | 'idle' | 'running' | 'draining';

/** Not configured: the route answers 404, or the simulator rejects the token (401). Neither is a session end. */
export function isNotConfigured(error: unknown): boolean {
  return error instanceof ApiError && (error.code === 'http.not_found' || error.code === 'simulator.unauthorized');
}

/** Network failures, timeouts and a bad or slow gateway: the simulator could not be reached. */
function isUnreachable(error: ApiError): boolean {
  return error.code === 'client.network' || error.code === 'client.timeout' || error.status === 502 || error.status === 504;
}

export function consoleMode(result: { data?: StatusResult; error?: unknown }, draining: boolean): ConsoleMode {
  const { data, error } = result;
  if (isNotConfigured(error)) return 'notConfigured';
  if (error !== undefined && error !== null) {
    if (error instanceof ApiError && error.code === 'auth.forbidden') return 'forbidden';
    // Any other answer from the simulator or the gateway (5xx, 400, 429…) is an error, not a lost connection.
    if (error instanceof ApiError && !isUnreachable(error)) return 'error';
    return 'unreachable';
  }
  if (!data) return 'loading';
  if (!data.status.running) return 'idle';
  return draining ? 'draining' : 'running';
}

export type Controls = {
  canStart: boolean;
  canStopNow: boolean;
  canDrain: boolean;
  canInject: boolean;
  canLeave: boolean;
  canFault: boolean;
};

/**
 * What the console enables (M2B-44, M2B-45). It only derives rules the status can know:
 * an implicit idle session still allows Start, and a 409 or 422 is shown as a banner.
 */
export function deriveControls(status: SimulatorStatus | undefined, draining: boolean): Controls {
  const running = status?.running ?? false;
  return {
    canStart: !draining && !(running && status?.scenario !== 'idle'),
    canInject: !draining,
    canStopNow: running,
    canDrain: running && !draining,
    canLeave: running,
    canFault: running && !draining,
  };
}
