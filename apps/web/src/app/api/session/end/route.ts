import type { NextRequest } from 'next/server';
import { handleSessionEnd } from '@/lib/session/session-end';

export async function GET(request: NextRequest) {
  return handleSessionEnd(request);
}
