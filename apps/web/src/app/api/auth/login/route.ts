import type { NextRequest } from 'next/server';
import { handleLogin } from '@/lib/bff/login';

export const POST = (request: NextRequest) => handleLogin(request);
