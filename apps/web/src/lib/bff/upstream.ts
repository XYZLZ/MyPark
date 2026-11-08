import { NextResponse, type NextRequest } from 'next/server';

const DEFAULT_API_URL = 'http://localhost:8080';

/** Absolute URL on the Go API. `path` starts with `/v1/`. */
export function apiUrl(path: string): string {
  const base = (process.env.API_INTERNAL_URL || DEFAULT_API_URL).replace(/\/+$/, '');
  return `${base}${path}`;
}

/** The incoming `X-Forwarded-For`, unchanged, or nothing when there is none (spec 5.7). */
export function forwardedHeaders(request: NextRequest): Record<string, string> {
  const forwarded = request.headers.get('x-forwarded-for');
  return forwarded ? { 'X-Forwarded-For': forwarded } : {};
}

/** An error envelope built here, for failures that never reached the API. The client translates `code`. */
export function bffError(status: number, code: string): NextResponse {
  return NextResponse.json(
    { error: { code, message: code, details: {} }, meta: { request_id: crypto.randomUUID() } },
    { status, headers: { 'Cache-Control': 'no-store' } },
  );
}

const PASSTHROUGH_HEADERS = ['content-type', 'retry-after', 'x-request-id'] as const;
const NULL_BODY_STATUSES = new Set([101, 204, 205, 304]);

/**
 * The upstream answer as a BFF response: status, body, `Retry-After` and
 * `X-Request-Id` pass through. Nothing else does, `Set-Cookie` least of all.
 */
export function passthrough(upstream: Response, body: string): NextResponse {
  const headers = new Headers({ 'Cache-Control': 'no-store' });
  for (const name of PASSTHROUGH_HEADERS) {
    const value = upstream.headers.get(name);
    if (value) headers.set(name, value);
  }
  return new NextResponse(NULL_BODY_STATUSES.has(upstream.status) ? null : body, { status: upstream.status, headers });
}
