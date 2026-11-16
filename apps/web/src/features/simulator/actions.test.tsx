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

beforeEach(() => {
  vi.useFakeTimers();
  restore = notifyQuerySynchronously();
});
afterEach(() => {
  restore();
  vi.useRealTimers();
});

const idle = () => jsonResponse(200, successBody(simulatorStatusIdleFixture));
const running = () => jsonResponse(200, successBody(simulatorStatusRunningFixture));
const stopped = () => jsonResponse(200, successBody({ ...simulatorStatusRunningFixture, running: false, scenario: '' }));
const noEvents = () => jsonResponse(200, successBody([]));

type Handlers = {
  status: Array<() => Response>;
  events?: () => Response;
  /** Keyed by `<last path segments>`, for example `cars` or `cars/car-000012/leave`. */
  posts?: Record<string, () => Response>;
};

/** Routes by path so a test scripts status polls, event polls and each POST. */
function route(handlers: Handlers) {
  let polls = 0;
  return stubFetch(async (url) => {
    const path = String(url).replace('/api/admin/simulator/', '');
    if (path === 'status') return handlers.status[Math.min(polls++, handlers.status.length - 1)]!();
    if (path.startsWith('events')) return (handlers.events ?? noEvents)();
    const post = handlers.posts?.[path];
    if (!post) throw new Error(`unexpected request: ${path}`);
    return post();
  });
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

async function renderConsole(locale: 'en' | 'es-DO' = 'en') {
  renderWithProviders(<SimulatorConsole lot={null} />, { locale });
  await settle();
}

const posts = (mock: ReturnType<typeof stubFetch>, path: string) => mock.mock.calls.filter(([url]) => String(url).endsWith(`/${path}`));
const statusCalls = (mock: ReturnType<typeof stubFetch>) => posts(mock, 'status');
const bodyOf = (call: ReturnType<typeof posts>[number]) => JSON.parse(String(call[1]!.body)) as unknown;
const submit = async (name: string) => {
  fireEvent.click(screen.getByRole('button', { name }));
  await settle();
};
const heldIn = () => screen.getByRole('region', { name: S.held.title });

async function injectHeld() {
  fireEvent.click(screen.getByLabelText(S.inject.hold));
  await submit(S.inject.submit);
}

const injected = { 'cars': () => jsonResponse(201, successBody({ car_id: 'car-000012' })) };

describe('inject', () => {
  test('inject is enabled while idle and a held car is listed', async () => {
    const fetchMock = route({ status: [idle], posts: injected });
    await renderConsole();
    expect(screen.getByRole('button', { name: S.inject.submit })).toBeEnabled();

    await injectHeld();

    expect(bodyOf(posts(fetchMock, 'cars')[0]!)).toEqual({ credential: 'ticket', hold: true });
    expect(heldIn()).toHaveTextContent('car-000012');
  });

  test('an unheld car is not listed and is acknowledged', async () => {
    route({ status: [idle], posts: injected });
    await renderConsole();
    await submit(S.inject.submit);
    expect(heldIn()).not.toHaveTextContent('car-000012');
    expect(screen.getByText('Car car-000012 added.')).toBeInTheDocument();
  });

  test('employee card requires the card number', async () => {
    const fetchMock = route({ status: [idle], posts: injected });
    await renderConsole();
    fireEvent.click(screen.getByLabelText(S.inject.employeeCard));
    await submit(S.inject.submit);

    expect(screen.getByText(en.validation.required)).toBeInTheDocument();
    expect(screen.getByLabelText(S.inject.card)).toHaveAttribute('aria-invalid', 'true');
    expect(posts(fetchMock, 'cars')).toHaveLength(0);
  });

  test('an employee card sends the card and the dwell minutes', async () => {
    const fetchMock = route({ status: [idle], posts: injected });
    await renderConsole();
    fireEvent.click(screen.getByLabelText(S.inject.employeeCard));
    fireEvent.change(screen.getByLabelText(S.inject.card), { target: { value: 'EMP-007' } });
    fireEvent.change(screen.getByLabelText(new RegExp(S.inject.dwell)), { target: { value: '45' } });
    await submit(S.inject.submit);

    expect(bodyOf(posts(fetchMock, 'cars')[0]!)).toEqual({ credential: 'employee_card', card: 'EMP-007', hold: false, dwell_minutes: 45 });
  });

  test('a card typed and then hidden is not sent', async () => {
    const fetchMock = route({ status: [idle], posts: injected });
    await renderConsole();
    fireEvent.click(screen.getByLabelText(S.inject.employeeCard));
    fireEvent.change(screen.getByLabelText(S.inject.card), { target: { value: 'nonsense' } });
    fireEvent.click(screen.getByLabelText(S.inject.ticket));
    await submit(S.inject.submit);
    expect(bodyOf(posts(fetchMock, 'cars')[0]!)).toEqual({ credential: 'ticket', hold: false });
  });

  test('a full lot (409) shows the translated message', async () => {
    route({ status: [idle], posts: { cars: () => jsonResponse(409, errorBody('gate.lot_full')) } });
    await renderConsole('es-DO');
    fireEvent.click(screen.getByRole('button', { name: esDO.simulator.inject.submit }));
    await settle();
    expect(screen.getByRole('alert')).toHaveTextContent('El estacionamiento está lleno.');
  });

  test('inject is disabled while draining, with a reason', async () => {
    route({ status: [running], posts: { 'scenarios/stop': () => jsonResponse(202, successBody(simulatorStatusRunningFixture)) } });
    await renderConsole();
    await submit(S.stop.drain);
    const button = screen.getByRole('button', { name: S.inject.submit });
    expect(button).toBeDisabled();
    expect(button.getAttribute('aria-describedby')).toBeTruthy();
    expect(screen.getByRole('button', { name: S.fault.submit })).toBeDisabled();
    expect(screen.getByRole('button', { name: new RegExp(`^${S.held.leaveOther}$`) })).toBeEnabled();
  });
});

describe('held cars', () => {
  const leaveRow = () => screen.getByRole('button', { name: 'Leave car-000012' });

  test('leave sends lose_ticket and removes the row', async () => {
    const fetchMock = route({ status: [running], posts: { ...injected, 'cars/car-000012/leave': () => jsonResponse(202, successBody({})) } });
    await renderConsole();
    await injectHeld();
    fireEvent.click(screen.getByLabelText('Lost ticket for car-000012'));
    fireEvent.click(leaveRow());
    await settle();

    expect(bodyOf(posts(fetchMock, 'cars/car-000012/leave')[0]!)).toEqual({ lose_ticket: true });
    expect(heldIn()).not.toHaveTextContent('car-000012');
  });

  test('leave without the lost ticket box sends false', async () => {
    const fetchMock = route({ status: [running], posts: { ...injected, 'cars/car-000012/leave': () => jsonResponse(202, successBody({})) } });
    await renderConsole();
    await injectHeld();
    fireEvent.click(leaveRow());
    await settle();
    expect(bodyOf(posts(fetchMock, 'cars/car-000012/leave')[0]!)).toEqual({ lose_ticket: false });
  });

  test('leave 404 car_not_found removes the row with a banner', async () => {
    route({ status: [running], posts: { ...injected, 'cars/car-000012/leave': () => jsonResponse(404, errorBody('simulator.car_not_found')) } });
    await renderConsole();
    await injectHeld();
    fireEvent.click(leaveRow());
    await settle();

    expect(screen.getByRole('alert')).toHaveTextContent(en.errors.simulator.car_not_found);
    expect(heldIn()).not.toHaveTextContent('car-000012');
  });

  test('another failure keeps the row and shows its banner', async () => {
    route({ status: [running], posts: { ...injected, 'cars/car-000012/leave': () => jsonResponse(409, errorBody('simulator.not_running')) } });
    await renderConsole();
    await injectHeld();
    fireEvent.click(leaveRow());
    await settle();
    expect(screen.getByRole('alert')).toHaveTextContent(en.errors.simulator.not_running);
    expect(heldIn()).toHaveTextContent('car-000012');
  });

  test('a typed car id must look like car-000123', async () => {
    const fetchMock = route({ status: [running] });
    await renderConsole();
    fireEvent.change(screen.getByLabelText(new RegExp(S.held.other)), { target: { value: 'car-12' } });
    await submit(S.held.leaveOther);

    expect(screen.getByText(en.validation.pattern)).toBeInTheDocument();
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes('/cars'))).toBe(true);
  });

  test('a typed car id leaves with its path', async () => {
    const fetchMock = route({ status: [running], posts: { 'cars/car-000123/leave': () => jsonResponse(202, successBody({})) } });
    await renderConsole();
    fireEvent.change(screen.getByLabelText(new RegExp(S.held.other)), { target: { value: 'car-000123' } });
    await submit(S.held.leaveOther);
    expect(bodyOf(posts(fetchMock, 'cars/car-000123/leave')[0]!)).toEqual({ lose_ticket: false });
  });

  test('every Leave control is disabled while one leave is pending', async () => {
    let finish: (response: Response) => void = () => undefined;
    route({
      status: [running],
      posts: { ...injected, 'cars/car-000012/leave': () => new Promise<Response>((resolve) => (finish = resolve)) as unknown as Response },
    });
    await renderConsole();
    await injectHeld();
    fireEvent.click(screen.getByRole('button', { name: 'Leave car-000012' }));
    await settle();
    expect(screen.getByRole('button', { name: S.held.leaveOther })).toBeDisabled();
    await act(async () => {
      finish(jsonResponse(202, successBody({})));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByRole('button', { name: S.held.leaveOther })).toBeEnabled();
  });

  test('leave is disabled while nothing runs', async () => {
    route({ status: [idle] });
    await renderConsole();
    expect(screen.getByRole('button', { name: S.held.leaveOther })).toBeDisabled();
  });

  test('held cars clear when the run ends', async () => {
    route({ status: [running, running, stopped], posts: injected });
    await renderConsole();
    await injectHeld();
    expect(heldIn()).toHaveTextContent('car-000012');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(heldIn()).not.toHaveTextContent('car-000012');
  });

  test('a car held while idle survives a takeover by a scenario start', async () => {
    route({ status: [running, running], posts: injected });
    await renderConsole();
    await injectHeld();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(heldIn()).toHaveTextContent('car-000012');
  });
});

describe('sensor fault', () => {
  const setCode = (value: string) => fireEvent.change(screen.getByLabelText(S.fault.code), { target: { value } });

  test('fault 204 shows Falla aplicada en E017', async () => {
    const fetchMock = route({ status: [running], posts: { 'sensors/E017/fault': () => new Response(null, { status: 204 }) } });
    await renderConsole('es-DO');
    fireEvent.change(screen.getByLabelText(esDO.simulator.fault.code), { target: { value: 'E017' } });
    fireEvent.click(screen.getByRole('button', { name: esDO.simulator.fault.submit }));
    await settle();

    expect(screen.getByText('Falla aplicada en E017')).toBeInTheDocument();
    expect(bodyOf(posts(fetchMock, 'sensors/E017/fault')[0]!)).toEqual({ fault: 'stuck_occupied' });
  });

  test('the chosen fault is sent', async () => {
    const fetchMock = route({ status: [running], posts: { 'sensors/E017/fault': () => new Response(null, { status: 204 }) } });
    await renderConsole();
    setCode('E017');
    fireEvent.change(screen.getByLabelText(S.fault.fault), { target: { value: 'none' } });
    await submit(S.fault.submit);
    expect(bodyOf(posts(fetchMock, 'sensors/E017/fault')[0]!)).toEqual({ fault: 'none' });
  });

  test('a code that is not E000 shape shows the pattern message and sends nothing', async () => {
    const fetchMock = route({ status: [running] });
    await renderConsole();
    setCode('17');
    await submit(S.fault.submit);
    expect(screen.getByText(en.validation.pattern)).toBeInTheDocument();
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes('/sensors'))).toBe(true);
  });

  test('fault 422 known_space maps to the code field', async () => {
    const body = errorBody('validation.failed', { fields: [{ field: 'code', rule: 'known_space' }] });
    route({ status: [running], posts: { 'sensors/E999/fault': () => jsonResponse(422, body) } });
    await renderConsole();
    setCode('E999');
    await submit(S.fault.submit);

    expect(screen.getByText(en.validation.known_space)).toBeInTheDocument();
    expect(screen.getByLabelText(S.fault.code)).toHaveAttribute('aria-invalid', 'true');
  });

  test('a fault 409 shows the translated banner', async () => {
    route({ status: [running], posts: { 'sensors/E017/fault': () => jsonResponse(409, errorBody('simulator.not_running')) } });
    await renderConsole();
    setCode('E017');
    await submit(S.fault.submit);
    expect(screen.getByRole('alert')).toHaveTextContent(en.errors.simulator.not_running);
  });

  test('fault is disabled while nothing runs, with a reason', async () => {
    route({ status: [idle] });
    await renderConsole();
    const button = screen.getByRole('button', { name: S.fault.submit });
    expect(button).toBeDisabled();
    expect(button.getAttribute('aria-describedby')).toBeTruthy();
  });
});

describe('overlapping and cancelled polls', () => {
  test('an events response from a cancelled fetch writes nothing and no row repeats', async () => {
    const row = (seq: number) => ({ seq, at: '2026-11-15T14:46:55Z', device: 'd', type: 't', result: 'ack' });
    let release: (response: Response) => void = () => undefined;
    let calls = 0;
    route({
      status: [running],
      posts: injected,
      events: () => {
        calls++;
        if (calls === 1) return jsonResponse(200, successBody([row(1)]));
        if (calls === 2) return new Promise<Response>((resolve) => (release = resolve)) as unknown as Response;
        return jsonResponse(200, successBody([row(1), row(2)]));
      },
    });
    await renderConsole();
    // Poll 2 is in flight when the inject refresh cancels it and sends poll 3 with the same cursor.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    await submit(S.inject.submit);
    await act(async () => {
      release(jsonResponse(200, successBody([row(2), row(3)])));
      await vi.advanceTimersByTimeAsync(0);
    });
    const shown = screen.getAllByRole('row').slice(1).map((r) => r.firstElementChild?.textContent);
    expect(shown).toEqual(['2', '1']);
  });
});

describe('after an action', () => {
  test('inject, leave and fault refetch status at once', async () => {
    const fetchMock = route({
      status: [running],
      posts: {
        ...injected,
        'cars/car-000012/leave': () => jsonResponse(202, successBody({})),
        'sensors/E017/fault': () => new Response(null, { status: 204 }),
      },
    });
    await renderConsole();
    let before = statusCalls(fetchMock).length;

    await injectHeld();
    expect(statusCalls(fetchMock)).toHaveLength(++before);

    fireEvent.click(screen.getByRole('button', { name: 'Leave car-000012' }));
    await settle();
    expect(statusCalls(fetchMock)).toHaveLength(++before);

    fireEvent.change(screen.getByLabelText(S.fault.code), { target: { value: 'E017' } });
    await submit(S.fault.submit);
    expect(statusCalls(fetchMock)).toHaveLength(++before);
  });

  test('focus moves to the status heading after a successful inject', async () => {
    route({ status: [idle], posts: injected });
    await renderConsole();
    await submit(S.inject.submit);
    expect(screen.getByRole('heading', { name: S.status.title })).toHaveFocus();
  });

  test('the event log polls while idle and sends the cursor as a digit string', async () => {
    const event = { seq: 1846, at: '2026-11-15T14:46:55Z', device: 'dispenser-1', type: 'barrier.passed', result: 'ack' };
    let calls = 0;
    const fetchMock = route({ status: [idle], events: () => (calls++ === 0 ? jsonResponse(200, successBody([event])) : noEvents()) });
    await renderConsole();
    expect(screen.getByRole('table')).toHaveTextContent('1846');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    const urls = fetchMock.mock.calls.map(([url]) => String(url)).filter((url) => url.includes('/events'));
    expect(urls).toEqual(['/api/admin/simulator/events?after=0', '/api/admin/simulator/events?after=1846']);
  });

  test('after a failed poll the log starts over from 0 without duplicating rows', async () => {
    const batch = (...seqs: number[]) =>
      jsonResponse(200, successBody(seqs.map((seq) => ({ seq, at: '2026-11-15T14:46:55Z', device: 'd', type: 't', result: 'ack' }))));
    const answers = [() => batch(1, 2), () => {
      throw new TypeError('offline');
    }, () => batch(1, 2, 3)];
    let n = 0;
    const fetchMock = route({ status: [idle], events: () => answers[Math.min(n++, 2)]!() });
    await renderConsole();
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_000);
      });
    }
    expect(screen.getAllByRole('row')).toHaveLength(1 + 3);
    expect(screen.queryByText(/skipped/)).not.toBeInTheDocument();
    const urls = fetchMock.mock.calls.map(([url]) => String(url)).filter((url) => url.includes('/events'));
    expect(urls).toEqual([
      '/api/admin/simulator/events?after=0',
      '/api/admin/simulator/events?after=2',
      '/api/admin/simulator/events?after=0',
    ]);
  });
});
