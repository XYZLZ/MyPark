import { errorBody, simulatorStatusRunningFixture, successBody } from '@mypark/contracts/fixtures';
import { describe, expect, test } from 'vitest';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { fetchStatus, startScenario, stopScenario } from './api';

const BIG_SEED_TEXT = JSON.stringify(successBody({ ...simulatorStatusRunningFixture, seed: 1 })).replace('"seed":1', '"seed":9007199254740993');

function bigSeedResponse(status = 200): Response {
  return new Response(BIG_SEED_TEXT, { status, headers: { 'Content-Type': 'application/json' } });
}

describe('simulator api', () => {
  test('fetchStatus keeps the raw seed digits', async () => {
    const fetchMock = stubFetch(bigSeedResponse());

    const result = await fetchStatus();

    expect(result.rawSeed).toBe('9007199254740993');
    expect(result.status.scenario).toBe('weekday');
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/admin/simulator/status');
  });

  test('startScenario posts the body text with the digits unquoted', async () => {
    const fetchMock = stubFetch(bigSeedResponse(202));

    await startScenario({ scenario: 'weekday', speed: 30, seed: '9007199254740993', start_at: '' });

    const [url, init] = fetchMock.mock.calls[0]!;
    if (!init) throw new Error('the request had no init');
    expect(url).toBe('/api/admin/simulator/scenarios/start');
    expect(init.method).toBe('POST');
    expect(init.body).toBe('{"scenario":"weekday","speed":30,"seed":9007199254740993}');
  });

  test('stopScenario posts the drain flag', async () => {
    const fetchMock = stubFetch(jsonResponse(202, successBody(simulatorStatusRunningFixture)));

    await stopScenario(true);

    expect(fetchMock.mock.calls[0]![0]).toBe('/api/admin/simulator/scenarios/stop');
    expect(fetchMock.mock.calls[0]![1]!.body).toBe('{"drain":true}');
  });

  test('an error envelope becomes an ApiError', async () => {
    stubFetch(jsonResponse(409, errorBody('simulator.already_running')));
    await expect(stopScenario(false)).rejects.toMatchObject({ code: 'simulator.already_running', status: 409 });
  });
});
