import type { NextRequest } from 'next/server';
import { handleSimulator } from '@/lib/simulator/handler';

type Context = { params: Promise<{ path: string[] }> };

const handle = async (request: NextRequest, { params }: Context) => handleSimulator(request, (await params).path);

export const GET = handle;
export const POST = handle;
