import { parseEnvelope, tokenResponseSchema, type LoginResult } from '@mypark/contracts';
import { NextResponse, type NextRequest } from 'next/server';
import { newCsrfToken, setCsrfCookie, setSessionCookies } from '@/lib/session/cookies';
import { ARGON2_TIMEOUT_MS } from './allowlist';
import { verifyMutation } from './csrf';
import { apiUrl, bffError, forwardedHeaders, passthrough } from './upstream';

const parseTokens = parseEnvelope(tokenResponseSchema);

/**
 * `POST /api/auth/login`. Calls Go with `client_kind: "web"` set here, puts the
 * tokens in httpOnly cookies and returns the user and the legal flag only, so a
 * token never reaches the browser's JavaScript (spec 5.5).
 */
export async function handleLogin(request: NextRequest): Promise<NextResponse> {
  const denied = verifyMutation(request);
  if (denied) return denied;

  let input: unknown;
  try {
    input = await request.json();
  } catch {
    return bffError(400, 'http.invalid_json');
  }
  if (typeof input !== 'object' || input === null || Array.isArray(input)) return bffError(400, 'http.invalid_json');
  const { email, password } = input as Record<string, unknown>;

  let upstream: Response;
  let text: string;
  try {
    upstream = await fetch(apiUrl('/v1/auth/login'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json', ...forwardedHeaders(request) },
      // Only these fields, and the client kind is not the browser's to choose.
      body: JSON.stringify({ email, password, client_kind: 'web' }),
      cache: 'no-store',
      signal: AbortSignal.timeout(ARGON2_TIMEOUT_MS),
    });
    text = await upstream.text();
  } catch {
    return bffError(502, 'internal.unexpected');
  }

  if (upstream.status !== 200) return passthrough(upstream, text);

  let tokens: ReturnType<typeof parseTokens>;
  let meta: unknown;
  try {
    const body: unknown = JSON.parse(text);
    tokens = parseTokens(body, 200);
    meta = (body as { meta: unknown }).meta;
  } catch {
    return bffError(502, 'internal.unexpected');
  }

  const data: LoginResult = { user: tokens.user, legal_acceptance_required: tokens.legal_acceptance_required };
  const response = NextResponse.json({ data, meta }, { headers: { 'Cache-Control': 'no-store' } });
  const requestId = upstream.headers.get('x-request-id');
  if (requestId) response.headers.set('X-Request-Id', requestId);

  setSessionCookies(response, {
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token,
    expiresIn: tokens.expires_in,
    refreshExpiresIn: tokens.refresh_expires_in,
  });
  setCsrfCookie(response, newCsrfToken());
  return response;
}
