import { ApiError, type SimulatorStatus } from '@mypark/contracts';
import { simulatorStatusIdleFixture, simulatorStatusRunningFixture } from '@mypark/contracts/fixtures';
import { describe, expect, test } from 'vitest';
import { consoleMode, deriveControls } from './controls';

const weekday: SimulatorStatus = simulatorStatusRunningFixture;
const implicitIdle: SimulatorStatus = { ...simulatorStatusRunningFixture, scenario: 'idle' };
const result = (status: SimulatorStatus) => ({ status, rawSeed: String(status.seed) });

describe('deriveControls', () => {
  test('idle allows start and inject only', () => {
    expect(deriveControls(simulatorStatusIdleFixture, false)).toEqual({
      canStart: true,
      canInject: true,
      canStopNow: false,
      canDrain: false,
      canLeave: false,
      canFault: false,
    });
  });

  test('a running non-idle scenario blocks start', () => {
    const controls = deriveControls(weekday, false);
    expect(controls.canStart).toBe(false);
    expect(controls.canStopNow && controls.canDrain && controls.canLeave && controls.canFault && controls.canInject).toBe(true);
  });

  test('the implicit idle session still allows start and inject', () => {
    const controls = deriveControls(implicitIdle, false);
    expect(controls.canStart).toBe(true);
    expect(controls.canInject).toBe(true);
  });

  test('draining leaves only Stop now and Leave', () => {
    expect(deriveControls(weekday, true)).toEqual({
      canStart: false,
      canInject: false,
      canStopNow: true,
      canDrain: false,
      canLeave: true,
      canFault: false,
    });
  });

  test('an unknown status allows start and inject only', () => {
    const controls = deriveControls(undefined, false);
    expect(controls).toMatchObject({ canStart: true, canInject: true, canStopNow: false, canLeave: false });
  });
});

describe('consoleMode', () => {
  const apiError = (code: string, status: number) => new ApiError({ code, message: code, status });

  test('maps 404 and simulator.unauthorized to notConfigured, network to unreachable', () => {
    expect(consoleMode({ error: apiError('http.not_found', 404) }, false)).toBe('notConfigured');
    expect(consoleMode({ error: apiError('simulator.unauthorized', 401) }, false)).toBe('notConfigured');
    expect(consoleMode({ error: apiError('client.network', 0) }, false)).toBe('unreachable');
    expect(consoleMode({ error: apiError('internal.unexpected', 502) }, false)).toBe('unreachable');
    expect(consoleMode({ error: apiError('client.timeout', 0) }, false)).toBe('unreachable');
    expect(consoleMode({ error: apiError('internal.unexpected', 504) }, false)).toBe('unreachable');
  });

  test('derives idle, running and draining from the data', () => {
    expect(consoleMode({ data: result(simulatorStatusIdleFixture) }, false)).toBe('idle');
    expect(consoleMode({ data: result(weekday) }, false)).toBe('running');
    expect(consoleMode({ data: result(weekday) }, true)).toBe('draining');
  });

  test('keeps the last data when a later poll fails with a transient error', () => {
    expect(consoleMode({ data: result(weekday), error: apiError('client.network', 0) }, false)).toBe('unreachable');
  });

  test('403 is forbidden, another 5xx is a server error, and no answer yet is loading', () => {
    expect(consoleMode({ error: apiError('auth.forbidden', 403) }, false)).toBe('forbidden');
    expect(consoleMode({ error: apiError('internal.unexpected', 500) }, false)).toBe('error');
    expect(consoleMode({}, false)).toBe('loading');
  });
});
