import type { SseFrame } from '@mypark/contracts';
import { availabilityReducedFixture, errorBody } from '@mypark/contracts/fixtures';

import { controllableStream } from '@/testing/stream';

import { openStream } from './sse';

const URL = 'http://api.test/v1/lots/L1/availability/stream?include=spaces';

function start() {
  const stream = controllableStream();
  const controller = new AbortController();
  const frames: SseFrame[] = [];
  const done = openStream(URL, { signal: controller.signal, onFrame: (f) => frames.push(f), fetchImpl: stream.fetchImpl });
  return { stream, controller, frames, done };
}

/** Lets the transport's awaits run; no timers are involved. */
const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

test('sends Accept text/event-stream and the abort signal', async () => {
  const { stream, controller, done } = start();
  expect(stream.calls).toEqual([{ url: URL, signal: controller.signal, headers: { Accept: 'text/event-stream' } }]);
  controller.abort();
  await expect(done).resolves.toEqual({ kind: 'aborted' });
});

test('chunked frames split mid-line are parsed once', async () => {
  const { stream, frames, done } = start();
  const data = JSON.stringify(availabilityReducedFixture);
  stream.push('event: availa');
  await settle();
  stream.push(`bility\ndata: ${data}\n`);
  await settle();
  expect(frames).toHaveLength(0);
  stream.push('\n');
  await settle();
  expect(frames).toEqual([{ event: 'availability', data }]);
  stream.end();
  await expect(done).resolves.toEqual({ kind: 'ended' });
  expect(frames).toHaveLength(1);
});

test('a multi-byte character split across chunks decodes once', async () => {
  const { stream, frames, done } = start();
  const bytes = new TextEncoder().encode('data: "Ñ"\n\n');
  const split = bytes.indexOf(0xc3) + 1;
  stream.pushBytes(bytes.slice(0, split));
  await settle();
  stream.pushBytes(bytes.slice(split));
  stream.end();
  await done;
  expect(frames).toEqual([{ event: 'message', data: '"Ñ"' }]);
});

test('a network failure while reading gives network', async () => {
  const { stream, done } = start();
  stream.push(': hi\n');
  await settle();
  stream.fail();
  await expect(done).resolves.toEqual({ kind: 'network' });
});

test('a non-200 returns status, code and retryAfterMs', async () => {
  const { stream, done } = start();
  stream.respond(429, JSON.stringify(errorBody('rate_limit.exceeded')), { 'Retry-After': '10' });
  await expect(done).resolves.toEqual({ kind: 'http', status: 429, code: 'rate_limit.exceeded', retryAfterMs: 10_000 });
});

test('an HTML error body gives code null', async () => {
  const { stream, done } = start();
  stream.respond(502, '<html>Bad gateway</html>', { 'content-type': 'text/html' });
  await expect(done).resolves.toEqual({ kind: 'http', status: 502, code: null, retryAfterMs: null });
});
