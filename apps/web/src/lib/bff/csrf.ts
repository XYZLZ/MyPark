import { timingSafeEqual } from 'node:crypto';
import type { NextRequest, NextResponse } from 'next/server';
import { CSRF_COOKIE } from '@/lib/session/cookies';
import { bffError } from './upstream';

const DEV_ORIGINS = 'http://localhost:3000,http://127.0.0.1:3000';

function allowedOrigins(): string[] {
  return (process.env.WEB_ALLOWED_ORIGINS || DEV_ORIGINS)
    .split(',')
    .map((origin) => origin.trim())
    .filter(Boolean);
}

function constantTimeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  return left.length === right.length && timingSafeEqual(left, right);
}

/**
 * Origin and double-submit check for POST, PATCH and DELETE (spec 5.4).
 * Returns the 403 to send, or null when the request may go on.
 */
export function verifyMutation(request: NextRequest): NextResponse | null {
  const origin = request.headers.get('origin');
  if (!origin || !allowedOrigins().includes(origin)) return bffError(403, 'auth.forbidden');

  const header = request.headers.get('x-csrf-token') ?? '';
  const cookie = request.cookies.get(CSRF_COOKIE)?.value ?? '';
  if (header === '' || cookie === '' || !constantTimeEqual(header, cookie)) return bffError(403, 'auth.forbidden');
  return null;
}
