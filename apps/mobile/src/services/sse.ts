import { fetch } from 'expo/fetch';
import { createSseParser, parseRetryAfter, type SseFrame } from '@mypark/contracts';

/** The slice of an `expo/fetch` response the stream reads. */
export type StreamResponse = {
  status: number;
  headers: { get(name: string): string | null };
  body: { getReader(): { read(): Promise<{ done: boolean; value?: Uint8Array }> } } | null;
  text(): Promise<string>;
};

export type StreamFetch = (url: string, init: { headers: Record<string, string>; signal: AbortSignal }) => Promise<StreamResponse>;

export type StreamEnd =
  | { kind: 'ended' }
  | { kind: 'aborted' }
  | { kind: 'network' }
  | { kind: 'http'; status: number; code: string | null; retryAfterMs: number | null };

const ENDED: StreamEnd = { kind: 'ended' };
const ABORTED: StreamEnd = { kind: 'aborted' };
const NETWORK: StreamEnd = { kind: 'network' };

const expoFetch: StreamFetch = fetch;

/** The `error.code` of a JSON error envelope; `null` for anything else, such as HTML from a proxy. */
function errorCode(text: string): string | null {
  try {
    const body: unknown = JSON.parse(text);
    const error = typeof body === 'object' && body !== null && 'error' in body ? body.error : null;
    const code = typeof error === 'object' && error !== null && 'code' in error ? error.code : null;
    return typeof code === 'string' ? code : null;
  } catch {
    return null;
  }
}

const isEventStream = (res: StreamResponse) =>
  res.status === 200 && res.body !== null && /^text\/event-stream\b/i.test(res.headers.get('content-type') ?? '');

/**
 * Opens the availability stream with `expo/fetch` (M2B-21) and feeds every frame to `onFrame` until
 * the body ends, fails or `signal` aborts. Not the M1 `http` client: its 3 s timeout would cut the
 * stream. Unlike `EventSource`, the status and `Retry-After` of a failed open are readable.
 * Never rejects: the result says how the stream ended.
 */
export async function openStream(
  url: string,
  { signal, onFrame, fetchImpl = expoFetch }: { signal: AbortSignal; onFrame: (frame: SseFrame) => void; fetchImpl?: StreamFetch },
): Promise<StreamEnd> {
  try {
    const res = await fetchImpl(url, { headers: { Accept: 'text/event-stream' }, signal });
    if (!isEventStream(res) || res.body === null) {
      const text = await res.text().catch(() => '');
      if (signal.aborted) return ABORTED;
      return { kind: 'http', status: res.status, code: errorCode(text), retryAfterMs: parseRetryAfter(res.headers.get('retry-after')) };
    }
    const reader = res.body.getReader();
    // `ignoreBOM` leaves the BOM to the parser, the one place that strips it (contracts sse/parser.ts).
    const decoder = new TextDecoder('utf-8', { ignoreBOM: true });
    const parser = createSseParser();
    for (;;) {
      const { done, value } = await reader.read();
      if (signal.aborted) return ABORTED;
      const text = done ? decoder.decode() : decoder.decode(value, { stream: true });
      for (const frame of parser.push(text)) {
        onFrame(frame);
        if (signal.aborted) return ABORTED;
      }
      if (done) return ENDED;
    }
  } catch {
    return signal.aborted ? ABORTED : NETWORK;
  }
}
