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
    if (String(url).includes('/events')) return jsonResponse(200, successBody([]));
    return handlers.post!();
  });
}

describe('SimulatorConsole', () => {
  test('shows the not configured panel on 404', async () => {
    stubFetch(jsonResponse(404, errorBody('http.not_found')));
    await renderConsole('es-DO');
    expect(screen.getAllByText(esDO.simulator.notConfigured)).toHaveLength(2);
    expect(screen.getAllByText('El simulador no está configurado en este servidor.')).toHaveLength(2);
  });

  test('an upstream 401 simulator.unauthorized shows the same panel and does not end the session', async () => {
    stubFetch(jsonResponse(401, errorBody('simulator.unauthorized')));
    await renderConsole();
    expect(screen.getAllByText(S.notConfigured)).toHaveLength(2);
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

    expect(fetchMock.mock.calls.every(([url]) => /\/(status|events)/.test(String(url)))).toBe(true);
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

describe('SimulatorConsole accessibility and states', () => {
  const heading = () => screen.getByRole('heading', { name: S.status.title });
  const liveText = () => screen.getAllByRole('status').map((el) => el.textContent ?? '');

  test('focus moves to the status heading after a successful start', async () => {
    route({ status: [idle, running], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.start.submit }));
    await settle();
    expect(heading()).toHaveFocus();
  });

  test('focus moves to the status heading after Stop now', async () => {
    route({ status: [running, stopped], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.stop.now }));
    await settle();
    expect(heading()).toHaveFocus();
  });

  test('focus moves to the status heading after Stop after cars leave', async () => {
    route({ status: [running], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.stop.drain }));
    await settle();
    expect(heading()).toHaveFocus();
  });

  test('one live region stays mounted and announces every mode change', async () => {
    stubFetch(idle());
    renderWithProviders(<SimulatorConsole lot={null} />, { locale: 'en' });
    const region = screen.getAllByRole('status')[0]!;
    expect(region.textContent).toBe('');

    await settle();

    expect(region.isConnected).toBe(true);
    expect(region).toHaveTextContent(S.status.idle);
  });

  test('an unreachable simulator is spoken by the alert only, never twice', async () => {
    stubFetch(async () => {
      throw new TypeError('offline');
    });
    await renderConsole();
    expect(screen.getByRole('alert')).toHaveTextContent(S.unreachable);
    expect(liveText().join(' ')).not.toContain(S.unreachable);
  });

  test('the live region announces not configured, and the panel itself is not a live region', async () => {
    stubFetch(jsonResponse(404, errorBody('http.not_found')));
    await renderConsole();
    expect(liveText()).toContain(S.notConfigured);
    const panel = screen.getByRole('heading', { name: S.status.title }).closest('section')!;
    expect(panel).toHaveTextContent(S.notConfigured);
    expect(panel).not.toHaveAttribute('role');
    expect(panel.closest('[role="status"],[role="alert"],[aria-live]')).toBeNull();
    expect(panel.querySelector('[role="status"],[role="alert"],[aria-live]')).toBeNull();
  });

  test('a 403 shows the forbidden message once', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.forbidden')));
    await renderConsole();
    expect(screen.getByRole('alert')).toHaveTextContent(S.forbidden);
    expect(liveText().join(' ')).not.toContain(S.forbidden);
  });

  test('a 500 shows the server error message once', async () => {
    stubFetch(jsonResponse(500, errorBody('internal.unexpected')));
    await renderConsole();
    expect(screen.getByRole('alert')).toHaveTextContent(S.serverError);
    expect(liveText().join(' ')).not.toContain(S.serverError);
  });

  test.each([
    ['400', 400, 'http.invalid_json'],
    ['429', 429, 'rate_limit.exceeded'],
  ])('a %s from the status poll is a generic error, not "could not reach"', async (_name, status, code) => {
    stubFetch(jsonResponse(status, errorBody(code)));
    await renderConsole();
    expect(screen.getByRole('alert')).toHaveTextContent(S.serverError);
    expect(screen.queryByText(S.unreachable)).not.toBeInTheDocument();
  });

  test('while the status is unknown the stop buttons say so, not that nothing is running', () => {
    stubFetch(() => new Promise<Response>(() => undefined));
    renderWithProviders(<SimulatorConsole lot={null} />, { locale: 'en' });
    const now = screen.getByRole('button', { name: S.stop.now });
    expect(now).toBeDisabled();
    expect(screen.queryByText(S.stop.notRunning)).not.toBeInTheDocument();
    expect(screen.getByText(S.stop.unknown)).toHaveAttribute('id', now.getAttribute('aria-describedby'));
  });

  test('when a failed action ends in the not configured panel, focus lands on its heading', async () => {
    route({ status: [running, () => jsonResponse(404, errorBody('http.not_found'))], post: () => jsonResponse(404, errorBody('http.not_found')) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.stop.now }));
    await settle();
    expect(screen.getAllByText(S.notConfigured).length).toBeGreaterThan(0);
    expect(screen.getByRole('heading', { name: S.status.title })).toHaveFocus();
  });

  test('the first load into not configured does not steal focus', async () => {
    stubFetch(jsonResponse(404, errorBody('http.not_found')));
    await renderConsole();
    expect(screen.getByRole('heading', { name: S.status.title })).not.toHaveFocus();
  });

  test('the last badge stays while a later poll fails', async () => {
    let calls = 0;
    stubFetch(async () => {
      if (calls++ === 0) return running();
      throw new TypeError('offline');
    });
    await renderConsole();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(screen.getByRole('alert')).toHaveTextContent(S.unreachable);
    expect(screen.getAllByText(S.status.running).length).toBeGreaterThan(0);
  });

  test('before the first answer Start is disabled and says the status is unknown', () => {
    stubFetch(() => new Promise<Response>(() => undefined));
    renderWithProviders(<SimulatorConsole lot={null} />, { locale: 'en' });
    expect(screen.getByRole('button', { name: S.start.submit })).toBeDisabled();
    const start = screen.getByRole('button', { name: S.start.submit });
    expect(document.getElementById(start.getAttribute('aria-describedby')!)).toHaveTextContent(S.start.disabledLoading);
    expect(screen.getByText(S.status.loading)).toBeInTheDocument();
  });

  test('disabled stop buttons give a visible reason tied with aria-describedby, and the drain hint only shows while draining', async () => {
    route({ status: [idle], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    const now = screen.getByRole('button', { name: S.stop.now });
    expect(now).toBeDisabled();
    expect(document.getElementById(now.getAttribute('aria-describedby')!)).toHaveTextContent(S.stop.notRunning);
    expect(screen.queryByText(S.stop.drainingHint)).not.toBeInTheDocument();
  });

  test('while draining the drain button is described by the hint', async () => {
    route({ status: [running], post: () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.stop.drain }));
    await settle();
    const drain = screen.getByRole('button', { name: S.stop.drain });
    expect(screen.getByText(S.stop.drainingHint)).toHaveAttribute('id', drain.getAttribute('aria-describedby'));
  });

  test('stops polling when it is not configured', async () => {
    const fetchMock = stubFetch(jsonResponse(404, errorBody('http.not_found')));
    await renderConsole();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    expect(statusCalls(fetchMock)).toHaveLength(1);
  });

  test('unmounting stops polling', async () => {
    const fetchMock = stubFetch(idle());
    const view = renderWithProviders(<SimulatorConsole lot={null} />, { locale: 'en' });
    await settle();
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    expect(statusCalls(fetchMock)).toHaveLength(1);
  });

  test.each([
    ['404', 404, 'http.not_found'],
    ['401 simulator.unauthorized', 401, 'simulator.unauthorized'],
  ])('a stop that gets %s shows the configuration message', async (_name, status, code) => {
    route({ status: [running], post: () => jsonResponse(status, errorBody(code)) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.stop.now }));
    await settle();
    expect(screen.getByRole('alert')).toHaveTextContent(S.notConfigured);
  });

  test('a start that gets 404 shows the configuration message', async () => {
    route({ status: [idle], post: () => jsonResponse(404, errorBody('http.not_found')) });
    await renderConsole();
    fireEvent.click(screen.getByRole('button', { name: S.start.submit }));
    await settle();
    expect(screen.getAllByText(S.notConfigured).length).toBeGreaterThan(0);
  });

  describe('copy seed', () => {
    function stubClipboard(writeText: () => Promise<void>) {
      vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    }

    test('shows Copied, announces it, and resets after a moment', async () => {
      stubClipboard(() => Promise.resolve());
      stubFetch(running());
      await renderConsole();
      fireEvent.click(screen.getByRole('button', { name: S.status.copySeed }));
      await settle();
      expect(screen.getByRole('button', { name: S.status.copied })).toBeInTheDocument();
      expect(liveText()).toContain(S.status.copied);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_500);
      });
      expect(screen.getByRole('button', { name: S.status.copySeed })).toBeInTheDocument();
    });

    test('tells the admin when the clipboard write fails', async () => {
      stubClipboard(() => Promise.reject(new Error('denied')));
      stubFetch(running());
      await renderConsole();
      fireEvent.click(screen.getByRole('button', { name: S.status.copySeed }));
      await settle();
      expect(liveText()).toContain(S.status.copyFailed);
      expect(screen.getAllByText(S.status.copyFailed).length).toBeGreaterThan(0);
    });
  });
});
