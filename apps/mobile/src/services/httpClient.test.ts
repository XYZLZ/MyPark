import { createHttpClient, getApiHealth } from './httpClient';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

test('shows unavailable on network error', async () => {
  const client = createHttpClient('http://x', jest.fn().mockRejectedValue(new TypeError('Network request failed')));
  expect(await getApiHealth(client)).toBe('unavailable');
});

test('createHttpClient rejects on network errors', async () => {
  const client = createHttpClient('http://x', jest.fn().mockRejectedValue(new TypeError('Network request failed')));
  await expect(client.get('/readyz')).rejects.toThrow('Network request failed');
});

test('getApiHealth probes /readyz and reports healthy', async () => {
  const fetchImpl = jest
    .fn()
    .mockResolvedValue(jsonResponse(200, { status: 'ready', checks: { database: 'ok', migrations: 'ok' } }));
  const client = createHttpClient('http://api.test', fetchImpl);
  expect(await getApiHealth(client)).toBe('healthy');
  expect(fetchImpl).toHaveBeenCalledWith('http://api.test/readyz', expect.objectContaining({ method: 'GET' }));
});

test('getApiHealth reports not_ready on a 503 health.not_ready envelope', async () => {
  const body = { error: { code: 'health.not_ready', message: 'migrations behind', request_id: 'r1' } };
  const client = createHttpClient('http://api.test', jest.fn().mockResolvedValue(jsonResponse(503, body)));
  expect(await getApiHealth(client)).toBe('not_ready');
});

test('a non-JSON body resolves instead of rejecting and maps to unavailable', async () => {
  const fetchImpl = jest.fn().mockResolvedValue(new Response('<html>Bad gateway</html>', { status: 502 }));
  const client = createHttpClient('http://api.test', fetchImpl);
  await expect(client.get('/readyz')).resolves.toEqual({ status: 502, body: undefined });
  expect(await getApiHealth(client)).toBe('unavailable');
});

test('a request that exceeds the timeout is aborted and maps to unavailable', async () => {
  const hangingFetch = jest.fn(
    (_url: string, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('Aborted')));
      }),
  );
  const client = createHttpClient('http://api.test', hangingFetch);
  await expect(client.get('/readyz', { timeoutMs: 10 })).rejects.toThrow('Aborted');
  expect(await getApiHealth(client)).toBe('unavailable');
});
