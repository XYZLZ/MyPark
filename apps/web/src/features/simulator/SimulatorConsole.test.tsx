import { errorBody, simulatorStatusIdleFixture, simulatorStatusRunningFixture, successBody } from '@mypark/contracts/fixtures';
import { act, fireEvent, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { notifyQuerySynchronously } from '@/testing/query';
import { renderWithProviders } from '@/testing/render';
import { SimulatorConsole } from './SimulatorConsole';

const S = en.simulator;
let restore: () => void;
let visibility: { mockRestore(): void } | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  restore = notifyQuerySynchronously();
});
afterEach(() => {
  restore();
  visibility?.mockRestore();
  visibility = undefined;
  vi.useRealTimers();
});

const idle = () => jsonResponse(200, successBody(simulatorStatusIdleFixture));
const running = () => jsonResponse(200, successBody(simulatorStatusRunningFixture));
const stopped = () => jsonResponse(200, successBody({ ...simulatorStatusRunningFixture, running: false, scenario: '' }));

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

async function renderConsole(locale: 'en' | 'es-DO' = 'en') {
  renderWithProviders(<SimulatorConsole lot={<p>lot panel</p>} />, { locale });
  await settle();
}

const statusCalls = (mock: ReturnType<typeof stubFetch>) => mock.mock.calls.filter(([url]) => String(url).endsWith('/status'));

/** Routes by path so a test can script status polls and the POSTs between them. */
function route(handlers: { status: Array<() => Response>; post?: () => Response }) {
  let polls = 0;
  return stubFetch(async (url) => {
    if (String(url).endsWith('/status')) return handlers.status[Math.min(polls++, handlers.status.length - 1)]!();
    return handlers.post!();
  });
}

describe('SimulatorConsole', () => {
  test('shows the not configured panel on 404', async () => {
    stubFetch(jsonResponse(404, errorBody('http.not_found')));
    await renderConsole('es-DO');
    expect(screen.getByText(esDO.simulator.notConfigured)).toBeInTheDocument();
    expect(screen.getByText('El simulador no está configurado en este servidor.')).toBeInTheDocument();
  });

  test('an upstream 401 simulator.unauthorized shows the same panel and does not end the session', async () => {
    stubFetch(jsonResponse(401, errorBody('simulator.unauthorized')));
    await renderConsole();
    expect(screen.getByText(S.notConfigured)).toBeInTheDocument();
  });

  test('polls status every 2 s while visible', async () => {
    const fetchMock = stubFetch(idle());
    await renderConsole();
    expect(statusCalls(fetchMock)).toHaveLength(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(statusCalls(fetchMock)).toHaveLength(2);
  });

  test('stops polling while the page is hidden', async () => {
    const fetchMock = stubFetch(idle());
    await renderConsole();
    visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'));
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    expect(statusCalls(fetchMock)).toHaveLength(1);
  });

  test('a start 409 shows the translated banner, never the raw code', async () => {
    route({ status: [idle], post: () => jsonResponse(409, errorBody('simulator.already_running')) });
    await renderConsole();

    fireEvent.click(screen.getByRole('button', { name: S.start.submit }));
    await settle();

    expect(screen.getByRole('alert')).toHaveTextContent(en.errors.simulator.already_running);
    expect(screen.queryByText('simulator.already_running')).not.toBeInTheDocument();
  });

  test('a successful start refetches status at once', async () => {
    const fetchMock = route({ status: [idle, running], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    expect(statusCalls(fetchMock)).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: S.start.submit }));
    await settle();

    expect(statusCalls(fetchMock)).toHaveLength(2);
    const start = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/scenarios/start'))!;
    expect(start[1]!.body).toBe('{"scenario":"weekday","speed":30}');
    expect(screen.getAllByText(S.status.running).length).toBeGreaterThan(0);
  });

  test('a running non-idle scenario disables Start and says why', async () => {
    stubFetch(running());
    await renderConsole();
    expect(screen.getByRole('button', { name: S.start.submit })).toBeDisabled();
    expect(screen.getByText(S.start.disabledRunning)).toBeInTheDocument();
  });

  test('a speed outside 1 to 120 shows the validation message and sends nothing', async () => {
    const fetchMock = stubFetch(idle());
    await renderConsole();

    fireEvent.change(screen.getByLabelText(new RegExp(S.start.speed)), { target: { value: '0' } });
    fireEvent.click(screen.getByRole('button', { name: S.start.submit }));
    await settle();

    expect(fetchMock.mock.calls.every(([url]) => String(url).endsWith('/status'))).toBe(true);
    expect(screen.getByText(/Must be at least 1/)).toBeInTheDocument();
  });

  test('draining disables Start and Stop after cars leave, keeps Stop now, and clears when not running', async () => {
    route({ status: [running, running, stopped], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();

    fireEvent.click(screen.getByRole('button', { name: S.stop.drain }));
    await settle();

    expect(screen.getByRole('button', { name: S.start.submit })).toBeDisabled();
    expect(screen.getByRole('button', { name: S.stop.drain })).toBeDisabled();
    expect(screen.getByRole('button', { name: S.stop.now })).toBeEnabled();
    expect(screen.getAllByText(S.status.draining).length).toBeGreaterThan(0);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });

    expect(screen.getByRole('button', { name: S.start.submit })).toBeEnabled();
    expect(screen.getByRole('button', { name: S.stop.now })).toBeDisabled();
  });

  test('a stop 409 shows the translated banner', async () => {
    route({ status: [running], post: () => jsonResponse(409, errorBody('simulator.not_running')) });
    await renderConsole();

    fireEvent.click(screen.getByRole('button', { name: S.stop.now }));
    await settle();

    expect(screen.getByRole('alert')).toHaveTextContent(en.errors.simulator.not_running);
  });

  test('the status panel shows a seed above 2^53 digit for digit', async () => {
    const text = JSON.stringify(successBody({ ...simulatorStatusRunningFixture, seed: 1 })).replace('"seed":1', '"seed":9007199254740993');
    stubFetch(new Response(text, { status: 200, headers: { 'Content-Type': 'application/json' } }));
    await renderConsole();

    expect(screen.getByText('9007199254740993')).toBeInTheDocument();
  });

  test('renders the lot slot beside the controls', async () => {
    stubFetch(idle());
    await renderConsole();
    expect(screen.getByText('lot panel')).toBeInTheDocument();
  });
});
