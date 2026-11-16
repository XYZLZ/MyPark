import 'server-only';
import { parseEnvelope, userSchema } from '@mypark/contracts';
import type { NextRequest, NextResponse } from 'next/server';
import { verifyMutation } from '@/lib/bff/csrf';
import { apiUrl, bffError, forwardedHeaders, passthrough } from '@/lib/bff/upstream';
import { ACCESS_COOKIE, REFRESH_COOKIE } from '@/lib/session/cookies';
import { matchSimulatorRoute } from './allowlist';
import { MAX_BODY_BYTES, SIMULATOR_TIMEOUT_MS, simulatorConfig, simulatorUrl } from './upstream';

const parseMe = parseEnvelope(userSchema);

/**
 * Null when the session belongs to an admin, otherwise the error to send.
 * Asked of Go on every request (M2B-27), so a demoted or revoked admin loses
 * the console at once. Anything but a parsed admin user is a denial.
 */
async function requireAdmin(request: NextRequest): Promise<NextResponse | null> {
  const access = request.cookies.get(ACCESS_COOKIE)?.value;
  if (!access) {
    return request.cookies.get(REFRESH_COOKIE)?.value ? bffError(503, 'internal.unexpected') : bffError(401, 'auth.unauthenticated');
  }
  let res: Response;
  let body: unknown;
  try {
    res = await fetch(apiUrl('/v1/me'), {
      headers: { Accept: 'application/json', Authorization: `Bearer ${access}`, ...forwardedHeaders(request) },
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(SIMULATOR_TIMEOUT_MS),
    });
    body = await res.json().catch(() => null);
  } catch {
    return bffError(502, 'internal.unexpected');
  }
  if (res.status === 401) return bffError(401, 'auth.unauthenticated');
  if (res.status === 403) return bffError(403, 'auth.forbidden');
  try {
    return parseMe(body, res.status).role === 'admin' ? null : bffError(403, 'auth.forbidden');
  } catch {
    return bffError(502, 'internal.unexpected');
  }
}

/** The raw body when it is a JSON object of at most MAX_BODY_BYTES, else the error to send. */
async function readBody(request: NextRequest): Promise<string | NextResponse> {
  const declared = Number(request.headers.get('content-length'));
  if (declared > MAX_BODY_BYTES) return bffError(413, 'http.payload_too_large');
  const text = await request.text();
  if (Buffer.byteLength(text, 'utf8') > MAX_BODY_BYTES) return bffError(413, 'http.payload_too_large');
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return bffError(400, 'http.invalid_json');
  }
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? text : bffError(400, 'http.invalid_json');
}

/**
 * The console BFF (design §7.1): config, allow-list, admin role, CSRF, body,
 * then the simulator call with the control token. The token is added here
 * only, never logged and never echoed; the client's cookies never go upstream.
 */
export async function handleSimulator(request: NextRequest, segments: string[]): Promise<NextResponse> {
  const config = simulatorConfig();
  if (!config) return bffError(404, 'http.not_found');

  const route = matchSimulatorRoute(request.method, segments, request.nextUrl.searchParams);
  if (!route) return bffError(404, 'http.not_found');

  const denied = await requireAdmin(request);
  if (denied) return denied;

  const headers: Record<string, string> = { Accept: 'application/json', 'X-Simulator-Token': config.token };
  let body: string | undefined;
  if (route.method === 'POST') {
    const csrf = verifyMutation(request);
    if (csrf) return csrf;
    const read = await readBody(request);
    if (typeof read !== 'string') return read;
    body = read;
    // The simulator answers 415 to anything else (PF1).
    headers['Content-Type'] = 'application/json';
  }

  try {
    const upstream = await fetch(simulatorUrl(config.baseUrl, route.upstreamPath), {
      method: route.method,
      headers,
      body,
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(SIMULATOR_TIMEOUT_MS),
    });
    return passthrough(upstream, await upstream.text());
  } catch {
    return bffError(502, 'internal.unexpected');
  }
}
