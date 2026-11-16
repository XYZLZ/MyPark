import { lotQueryKeys } from '@mypark/contracts';
import {
  availabilityClosedFixture,
  spacesFullEventFixture,
  availabilityFullFixture,
  availabilityReducedFixture,
  heartbeatFixture,
  lotFixture,
  sseText,
} from '@mypark/contracts/fixtures';
import { focusManager, QueryClient } from '@tanstack/react-query';
import { act, cleanup, fireEvent, screen, within } from '@testing-library/react-native';
import { AccessibilityInfo, Linking, Text } from 'react-native';

import { getApiBaseUrl } from '@/lib/env';
import { http } from '@/services/runtime';
import { requestsFor, stubLotApi } from '@/testing/lot-api';
import { notifyQuerySynchronously } from '@/testing/query';
import { renderScreen } from '@/testing/render';
import { controllableStream, type ControllableStream } from '@/testing/stream';

import { LotLive } from './LotLive';
import { acquireLiveLot, resetLiveLotsForTests } from './live/manager';

const mockRouter = { push: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);
const NOW = Date.parse('2026-11-15T14:50:00Z');

let queryClient: QueryClient;
let stream: ControllableStream;
let restoreNotify: () => void;
let releaseDriver: () => void;
let announce: jest.SpyInstance;

beforeEach(() => {
  jest.useFakeTimers({ now: NOW });
  restoreNotify = notifyQuerySynchronously();
  request.mockReset();
  mockRouter.push.mockReset();
  stubLotApi(request);
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  stream = controllableStream();
  // The first acquirer's deps drive the shared connection; the screen then joins it.
  releaseDriver = acquireLiveLot(getApiBaseUrl(), lotFixture.id, queryClient, {
    fetchImpl: stream.fetchImpl,
    random: () => 1,
    fetchSnapshot: () => new Promise(() => {}),
  }).release;
  announce = jest.spyOn(AccessibilityInfo, 'announceForAccessibility').mockImplementation(() => {});
});

afterEach(async () => {
  await cleanup();
  releaseDriver();
  resetLiveLotsForTests();
  restoreNotify();
  announce.mockRestore();
  jest.restoreAllMocks();
  jest.useRealTimers();
});

const renderLot = async () => {
  await renderScreen(<LotLive mapHref="/home/map" />, { language: 'es-DO', queryClient });
  return screen.findByText('Espacios disponibles');
};
const pushAvailability = (patch: object, version: number) =>
  act(async () => stream.push(sseText('availability', { ...availabilityReducedFixture, ...patch, version }, version)));

test('frame to screen: a pushed availability frame updates the count without a timer (M2B-42)', async () => {
  await renderLot();
  expect(screen.getByText('211')).toBeOnTheScreen();
  expect(screen.getByText('Conectando…')).toBeOnTheScreen();

  await pushAvailability({ available: 210 }, 121);

  // The frame reached the screen inside one act with no timer advanced. (Timers do get created: the stream's watchdog and the
  // announcer's re-check at its `dueAt`; a count of them would pin those internals, not the behavior.)
  expect(screen.getByText('210')).toBeOnTheScreen();
  expect(screen.getByText(/En tiempo real/)).toBeOnTheScreen();
});

test('shows the count, status, out of service, lot info and the six rates', async () => {
  await renderLot();
  expect(screen.getByText('de 277 espacios · 3 fuera de servicio')).toBeOnTheScreen();
  expect(screen.getByText('Abierto')).toBeOnTheScreen();
  expect(await screen.findByText(lotFixture.address)).toBeOnTheScreen();
  expect(screen.getByText('Abierto 24 horas')).toBeOnTheScreen();
  for (const row of ['RD$50.00', 'RD$25.00', 'RD$400.00', 'RD$500.00']) expect(await screen.findByText(row)).toBeOnTheScreen();
  expect(screen.getByText('10 min')).toBeOnTheScreen();
  expect(screen.getByText('15 min')).toBeOnTheScreen();
  for (const label of ['Primera hora', 'Hora adicional', 'Máximo por día', 'Salida sin costo', 'Salir tras pagar', 'Ticket perdido'])
    expect(screen.getByText(label)).toBeOnTheScreen();
});

test('stale keeps the last values and shows the frame time', async () => {
  await renderLot();

  await act(async () => {
    stream.push(sseText('heartbeat', heartbeatFixture));
    jest.setSystemTime(Date.parse('2026-11-15T14:52:00Z'));
  });
  await act(async () => stream.fail());

  expect(screen.getByText('211')).toBeOnTheScreen();
  expect(screen.getByText('Desactualizado — última actualización 10:52 a. m.')).toBeOnTheScreen();
  expect(screen.getByText('Reconectando…')).toBeOnTheScreen();
});

test('closed replaces the number with Cerrado', async () => {
  stubLotApi(request, availabilityClosedFixture);
  await renderLot();

  expect(screen.queryByText('211')).toBeNull();
  expect(screen.getAllByText('Cerrado').length).toBeGreaterThanOrEqual(2); // the number's place and the pill
  expect(screen.getByText('El parqueo no recibe vehículos ahora.')).toBeOnTheScreen();
});

test('full shows Lleno and 0 of 277', async () => {
  stubLotApi(request, availabilityFullFixture);
  await renderLot();

  expect(screen.getByText('Lleno')).toBeOnTheScreen();
  expect(screen.getByText('0')).toBeOnTheScreen();
  expect(screen.getByText('de 277 espacios · 3 fuera de servicio')).toBeOnTheScreen();
});

test('the count never grows past 1.5x font scale', async () => {
  await renderLot();
  expect(screen.getByText('211').props.maxFontSizeMultiplier).toBe(1.5);
});

test('the count card is one screen-reader element that says the count, the out of service figure and the status once', async () => {
  await renderLot();
  expect(screen.getByLabelText('Espacios disponibles: 211 de 277, 3 fuera de servicio. Abierto')).toBeOnTheScreen();
});

test('a closed card says why in its screen-reader label', async () => {
  stubLotApi(request, availabilityClosedFixture);
  await renderLot();
  expect(screen.getByLabelText('Espacios disponibles: Cerrado. El parqueo no recibe vehículos ahora.')).toBeOnTheScreen();
});

test('each rate is one element named "label: value"', async () => {
  await renderLot();
  expect(await screen.findByLabelText('Primera hora: RD$50.00')).toBeOnTheScreen();
});

test('Abrir en Maps is an outline link at block width', async () => {
  await renderLot();
  const link = await screen.findByRole('link', { name: 'Abrir en Maps' });
  expect(link.props.className).toContain('border-brand-navy');
});

test('Abrir en Maps opens the server map_url and says what it does', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  await renderLot();

  const link = await screen.findByRole('link', { name: 'Abrir en Maps' });
  expect(link.props.accessibilityHint).toBe('Abre Maps');
  await fireEvent.press(link);

  expect(open).toHaveBeenCalledWith(lotFixture.map_url);
});

test('renders the actions right under the count card', async () => {
  await renderScreen(<LotLive mapHref="/home/map" actions={<Text>acciones</Text>} />, { language: 'es-DO', queryClient });
  await screen.findByText('Espacios disponibles');

  const order = JSON.stringify(screen.toJSON());
  expect(order.indexOf('Espacios disponibles')).toBeLessThan(order.indexOf('acciones'));
  expect(order.indexOf('acciones')).toBeLessThan(order.indexOf('Información del parqueo'));
});

describe('the polite announcement', () => {
  test('is silent on mount, says a status change at once and a count change at most every 30 s', async () => {
    await renderLot();
    expect(announce).not.toHaveBeenCalled();

    await pushAvailability({ ...availabilityFullFixture, version: 121 }, 121);
    expect(announce).toHaveBeenLastCalledWith('El parqueo está lleno.');

    await act(async () => {
      await jest.advanceTimersByTimeAsync(5_000);
    });
    await pushAvailability({ available: 4, status: 'open' }, 122);
    // The status is said at once; the count of 4 waits for the 30 s window.
    expect(announce).toHaveBeenLastCalledWith('El parqueo está abierto.');
    expect(announce).not.toHaveBeenCalledWith(expect.stringContaining('Espacios disponibles: 4'));

    await act(async () => {
      await jest.advanceTimersByTimeAsync(25_000);
    });
    expect(announce).toHaveBeenLastCalledWith('Espacios disponibles: 4 de 277.');
  });

  test('a due count does not overwrite a status said a moment before', async () => {
    await renderLot();
    await pushAvailability({ available: 200 }, 121); // count held back: due at +30 s
    await act(async () => {
      await jest.advanceTimersByTimeAsync(29_000);
    });
    await pushAvailability({ available: 200, status: 'full' }, 122);
    expect(announce).toHaveBeenLastCalledWith('El parqueo está lleno.');
    announce.mockClear();

    await act(async () => {
      await jest.advanceTimersByTimeAsync(1_000); // the count is due, the status is 1 s old
    });
    expect(announce).not.toHaveBeenCalled();

    await act(async () => {
      await jest.advanceTimersByTimeAsync(3_000);
    });
    expect(announce).toHaveBeenLastCalledWith('Espacios disponibles: 200 de 277.');
  });
});

test('a foreground refetches the lot but never the stream-owned availability and spaces', async () => {
  await renderLot();
  await screen.findByText(lotFixture.address);
  const before = { availability: requestsFor(request, '/availability'), spaces: requestsFor(request, '/spaces'), lot: requestsFor(request, lotFixture.id) };
  expect(queryClient.getQueryState(lotQueryKeys.availability(lotFixture.id))?.status).toBe('success');

  // Ten minutes later every default-stale query is stale; the lot's own 5 minutes are over too.
  jest.setSystemTime(NOW + 600_000);
  await act(async () => {
    focusManager.setFocused(false);
    focusManager.setFocused(true);
  });

  expect(requestsFor(request, lotFixture.id)).toBe(before.lot + 1);
  expect(requestsFor(request, '/availability')).toBe(before.availability);
  expect(requestsFor(request, '/spaces')).toBe(before.spaces);
});

test('heartbeat keeps the live line', async () => {
  await renderLot();
  await act(async () => stream.push(sseText('heartbeat', heartbeatFixture)));
  expect(screen.getByText(/En tiempo real/)).toBeOnTheScreen();
});

const hidden = { includeHiddenElements: true } as const;

test('frame to screen: a spaces delta changes the cell without a timer (M2B-42)', async () => {
  await renderLot();
  await act(async () => stream.push(sseText('spaces', spacesFullEventFixture)));
  // E100 is past the 40 the preview draws, so the delta frees E010, which is in it.
  expect(within(screen.getByTestId('space-E010', hidden)).getByTestId('shape-car', hidden)).toBeTruthy();

  await act(async () => stream.push(sseText('spaces', { full: false, spaces: [{ code: 'E010', state: 'free' }] })));

  expect(within(screen.getByTestId('space-E010', hidden)).getByTestId('shape-free', hidden)).toBeTruthy();
});

test('the preview shows the first 40 spaces and says 40 de 280 espacios', async () => {
  await renderLot();
  expect(await screen.findByTestId('space-E040', hidden)).toBeTruthy();
  expect(screen.queryByTestId('space-E041', hidden)).toBeNull();
  expect(screen.getByText('40 de 280 espacios')).toBeOnTheScreen();
});

test('Ver mapa completo opens the full map', async () => {
  await renderLot();
  await fireEvent.press(await screen.findByRole('button', { name: 'Ver mapa completo' }));
  expect(mockRouter.push).toHaveBeenCalledWith('/home/map');
});

test('the map is hidden from accessibility and the summary covers all 280', async () => {
  await renderLot();
  const grid = await screen.findByTestId('space-grid', hidden);
  expect(grid.props.importantForAccessibility).toBe('no-hide-descendants');
  expect(grid.props.accessibilityElementsHidden).toBe(true);
  expect(screen.getByText('Sensores — libres: 211, ocupados: 66, fuera de servicio: 3.')).toBeOnTheScreen();
});

test('the preview dims with Actualizando el mapa… when the stream drops, but not on the server data alone', async () => {
  await renderLot();
  await screen.findByTestId('space-grid', hidden);
  expect(screen.queryByText('Actualizando el mapa…')).toBeNull(); // connecting: the snapshot is all there is
  await act(async () => stream.push(sseText('heartbeat', heartbeatFixture)));
  await act(async () => stream.fail());
  expect(screen.getByText('Actualizando el mapa…')).toBeOnTheScreen();
});
