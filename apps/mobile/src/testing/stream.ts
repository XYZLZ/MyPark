import type { StreamFetch, StreamResponse } from '@/services/sse';

type Read = { done: boolean; value?: Uint8Array };

type Connection = {
  settle(response: StreamResponse): void;
  reject(error: Error): void;
  settled: boolean;
  chunks: Uint8Array[];
  ended: boolean;
  error: Error | null;
  waiting: { resolve(read: Read): void; reject(error: Error): void } | null;
};

const abortError = () => Object.assign(new Error('The operation was aborted.'), { name: 'AbortError' });

/**
 * A controllable `expo/fetch` for stream tests. Every call opens a pending connection; the helpers act
 * on the latest one. `push` answers it with `200 text/event-stream` (once) and delivers UTF-8 bytes,
 * `respond` answers it with any other response, `end` finishes the body, `fail` drops the network.
 * Aborting the signal rejects the pending fetch and the pending read, as `expo/fetch` does.
 */
export function controllableStream() {
  const calls: { url: string; signal: AbortSignal; headers: Record<string, string> }[] = [];
  const connections: Connection[] = [];
  const encoder = new TextEncoder();

  function flush(conn: Connection): void {
    const waiting = conn.waiting;
    if (!waiting) return;
    if (conn.error) {
      conn.waiting = null;
      waiting.reject(conn.error);
    } else if (conn.chunks.length > 0) {
      conn.waiting = null;
      waiting.resolve({ done: false, value: conn.chunks.shift() });
    } else if (conn.ended) {
      conn.waiting = null;
      waiting.resolve({ done: true });
    }
  }

  function latest(): Connection {
    const conn = connections.at(-1);
    if (!conn) throw new Error('controllableStream: fetch was never called');
    return conn;
  }

  function streamResponse(conn: Connection): StreamResponse {
    return {
      status: 200,
      headers: { get: (name) => (name.toLowerCase() === 'content-type' ? 'text/event-stream; charset=utf-8' : null) },
      body: {
        getReader: () => ({
          read: () =>
            new Promise<Read>((resolve, reject) => {
              conn.waiting = { resolve, reject };
              flush(conn);
            }),
        }),
      },
      text: () => Promise.reject(new Error('the body is a stream')),
    };
  }

  function pushBytes(bytes: Uint8Array): void {
    const conn = latest();
    if (!conn.settled) conn.settle(streamResponse(conn));
    conn.chunks.push(bytes);
    flush(conn);
  }

  const fetchImpl: StreamFetch = (url, init) =>
    new Promise<StreamResponse>((resolve, reject) => {
      calls.push({ url, signal: init.signal, headers: init.headers });
      const conn: Connection = {
        settled: false,
        settle(response) {
          conn.settled = true;
          resolve(response);
        },
        reject(error) {
          conn.settled = true;
          reject(error);
        },
        chunks: [],
        ended: false,
        error: null,
        waiting: null,
      };
      connections.push(conn);
      init.signal.addEventListener('abort', () => {
        if (!conn.settled) conn.reject(abortError());
        conn.error = abortError();
        flush(conn);
      });
    });

  return {
    fetchImpl,
    calls,
    push: (text: string) => pushBytes(encoder.encode(text)),
    /** Raw bytes, e.g. half of a multi-byte character. */
    pushBytes,
    end() {
      const conn = latest();
      if (!conn.settled) conn.settle(streamResponse(conn));
      conn.ended = true;
      flush(conn);
    },
    fail() {
      const conn = latest();
      const error = new TypeError('Network request failed');
      if (!conn.settled) conn.reject(error);
      conn.error = error;
      flush(conn);
    },
    respond(status: number, body: string, headers: Record<string, string> = {}) {
      const lower = new Map(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
      latest().settle({
        status,
        headers: { get: (name) => lower.get(name.toLowerCase()) ?? null },
        body: null,
        text: () => Promise.resolve(body),
      });
    },
  };
}

export type ControllableStream = ReturnType<typeof controllableStream>;
