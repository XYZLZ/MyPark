import { describe, expect, test } from 'vitest';
import { matchSimulatorRoute } from './allowlist';

const match = (method: string, path: string, query = '') =>
  matchSimulatorRoute(method, path.split('/'), new URLSearchParams(query));

describe('matchSimulatorRoute', () => {
  test('matches the seven console routes', () => {
    expect(match('GET', 'status')).toEqual({ method: 'GET', upstreamPath: '/control/status' });
    expect(match('POST', 'scenarios/start')).toEqual({ method: 'POST', upstreamPath: '/control/scenarios/start' });
    expect(match('POST', 'scenarios/stop')).toEqual({ method: 'POST', upstreamPath: '/control/scenarios/stop' });
    expect(match('POST', 'cars')).toEqual({ method: 'POST', upstreamPath: '/control/cars' });
    expect(match('POST', 'cars/car-000123/leave')).toEqual({ method: 'POST', upstreamPath: '/control/cars/car-000123/leave' });
    expect(match('POST', 'sensors/E017/fault')).toEqual({ method: 'POST', upstreamPath: '/control/sensors/E017/fault' });
    expect(match('GET', 'events')).toEqual({ method: 'GET', upstreamPath: '/control/events' });
  });

  test('wrong methods and unknown paths are null', () => {
    expect(match('POST', 'status')).toBeNull();
    expect(match('GET', 'cars')).toBeNull();
    expect(match('HEAD', 'status')).toBeNull();
    expect(match('DELETE', 'cars')).toBeNull();
    expect(match('GET', 'status/extra')).toBeNull();
    expect(match('POST', 'scenarios')).toBeNull();
  });

  test('rejects crafted ids', () => {
    for (const path of [
      'cars/CAR-000001/leave',
      'cars/car-1/leave',
      'cars/car-0000001/leave',
      'cars/car-000001%2F..%2Fstatus/leave',
      'sensors/E1000/fault',
      'sensors/e017/fault',
      'sensors/E017 /fault',
    ]) {
      expect(match('POST', path), path).toBeNull();
    }
    // The segment as Next may hand it over, already decoded.
    expect(matchSimulatorRoute('POST', ['cars', 'car-000001/../status', 'leave'], new URLSearchParams())).toBeNull();
  });

  test('after must be one non-negative int64', () => {
    for (const query of ['after=-1', 'after=1e3', 'after=99999999999999999999', 'after=9223372036854775808', 'after=1&after=2', 'after=', 'after=01', 'after=+1']) {
      expect(match('GET', 'events', query), query).toBeNull();
    }
    expect(match('GET', 'events', 'after=0')?.upstreamPath).toBe('/control/events?after=0');
    expect(match('GET', 'events', 'after=9223372036854775807')?.upstreamPath).toBe('/control/events?after=9223372036854775807');
  });

  test('unknown query params are dropped', () => {
    expect(match('GET', 'status', 'x=1')?.upstreamPath).toBe('/control/status');
    expect(match('GET', 'events', 'after=5&x=1')?.upstreamPath).toBe('/control/events?after=5');
    expect(match('POST', 'cars', 'after=5&token=x')?.upstreamPath).toBe('/control/cars');
  });
});
