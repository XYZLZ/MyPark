import 'server-only';

export const SIMULATOR_TIMEOUT_MS = 5_000;
export const MAX_BODY_BYTES = 4_096;

/**
 * Where the simulator control API is and its token, or null when either is
 * missing (M2B-29). Both are server-only runtime variables: never `NEXT_PUBLIC_*`,
 * so the token never reaches a client bundle.
 */
export function simulatorConfig(env: NodeJS.ProcessEnv = process.env): { baseUrl: string; token: string } | null {
  const baseUrl = (env.SIMULATOR_INTERNAL_URL ?? '').replace(/\/+$/, '');
  const token = env.SIMULATOR_CONTROL_TOKEN ?? '';
  return baseUrl && token ? { baseUrl, token } : null;
}

/** Absolute URL on the simulator. `upstreamPath` comes from `matchSimulatorRoute` only. */
export function simulatorUrl(baseUrl: string, upstreamPath: string): string {
  return `${baseUrl}${upstreamPath}`;
}
