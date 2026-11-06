import 'server-only';
import { toHealthState, type ApiHealthState, type HealthProbeResult } from '@mypark/contracts';

const HEALTH_TIMEOUT_MS = 2000;

async function probeReadyz(baseUrl: string): Promise<HealthProbeResult> {
  try {
    const res = await fetch(`${baseUrl}/readyz`, {
      cache: 'no-store',
      signal: AbortSignal.timeout(HEALTH_TIMEOUT_MS),
    });
    const body: unknown = await res.json().catch(() => null);
    return { status: res.status, body };
  } catch {
    // Connection refused, DNS failure or timeout.
    return { networkError: true };
  }
}

/**
 * Reads the API readiness for the current request. Never throws: every
 * outcome, including an unreachable API, maps to an ApiHealthState.
 */
export async function getApiHealth(): Promise<ApiHealthState> {
  const baseUrl = process.env.API_INTERNAL_URL;
  if (!baseUrl) {
    console.error('API_INTERNAL_URL is not set; reporting the API as unavailable.');
    return toHealthState({ networkError: true });
  }
  return toHealthState(await probeReadyz(baseUrl));
}
