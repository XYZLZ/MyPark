import { ApiError, type SimulatorStatus } from '@mypark/contracts';
import type { StatusResult } from './api';

export type ConsoleMode = 'notConfigured' | 'unreachable' | 'idle' | 'running' | 'draining';

/** Not configured: the route answers 404, or the simulator rejects the token (401). Neither is a session end. */
function isNotConfigured(error: ApiError): boolean {
  return error.code === 'http.not_found' || error.code === 'simulator.unauthorized';
}

export function consoleMode(result: { data?: StatusResult; error?: unknown }, draining: boolean): ConsoleMode {
  const { data, error } = result;
  if (error instanceof ApiError && isNotConfigured(error)) return 'notConfigured';
  if (error !== undefined) return 'unreachable';
  if (!data?.status.running) return 'idle';
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
