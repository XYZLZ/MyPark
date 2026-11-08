import type { NextRequest } from 'next/server';
import { handleBff } from '@/lib/bff/handler';

type Context = { params: Promise<{ path: string[] }> };

const handle = async (request: NextRequest, { params }: Context) => handleBff(request, (await params).path);

export const GET = handle;
export const POST = handle;
export const PATCH = handle;
export const DELETE = handle;
