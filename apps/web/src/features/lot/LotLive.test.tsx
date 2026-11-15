import {
  availabilityClosedFixture,
  availabilityFullFixture,
  availabilityReducedFixture,
  heartbeatFixture,
  lotFixture,
  spacesDeltaFixture,
  spacesFixture,
  spacesFullEventFixture,
  tariffFixture,
} from '@mypark/contracts/fixtures';
import { act, cleanup, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { resetLiveLotsForTests } from '@/features/lot/live/manager';
import { FakeEventSource } from '@/testing/event-source';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { notifyQuerySynchronously } from '@/testing/query';
import { renderWithProviders } from '@/testing/render';
import { LotLive } from './LotLive';
import type { LotSnapshot } from './server';

const ORIGIN = 'http://api.test';
const NOW = Date.parse('2026-11-15T14:50:00Z');

let restoreNotify: () => void;

beforeEach(() => {
  vi.useFakeTimers({ now: NOW });
  vi.stubGlobal('EventSource', FakeEventSource);
  restoreNotify = notifyQuerySynchronously();
});

afterEach(() => {
  // Unmount before the reset (setup.ts cleans up later, as afterEach hooks run in reverse).
  cleanup();
  resetLiveLotsForTests();
  FakeEventSource.instances = [];
  restoreNotify();
  vi.useRealTimers();
});

const snapshot = (availability = availabilityReducedFixture): LotSnapshot => ({
  lot: lotFixture,
  availability,
  spaces: spacesFixture.spaces,
  tariff: tariffFixture,
  fetchedAt: NOW,
});

function renderHome(availability = availabilityReducedFixture, apiOrigin: string | null = ORIGIN) {
  return renderWithProviders(
    <LotLive variant="home" lotId={lotFixture.id} apiOrigin={apiOrigin} snapshot={snapshot(availability)} />,
    { locale: 'es-DO' },
  );
}

const newer = (available: number, version: number, afterMs: number): LotSnapshot => ({
  ...snapshot({ ...availabilityReducedFixture, available, version }),
  fetchedAt: NOW + afterMs,
});
const homeProps = (apiOrigin: string | null, snap: LotSnapshot) => (
  <LotLive variant="home" lotId={lotFixture.id} apiOrigin={apiOrigin} snapshot={snap} />
);

/** The value in a stat tile, found through its label (the legend repeats some labels). */
const tile = (label: string) => within(screen.getAllByText(label).find((el) => el.tagName === 'DT')!.closest('div')!);

describe('LotLive home', () => {
  test('frame to screen: a spaces delta changes the cell state without a timer (M2B-42)', () => {
    const { container } = renderHome();
    act(() => {
      FakeEventSource.latest().open();
      FakeEventSource.latest().emit('spaces', spacesFullEventFixture);
    });
    const timers = vi.getTimerCount();
    expect(container.querySelector<HTMLElement>('[data-code="E100"]')!.dataset.state).not.toBe('occupied');

    act(() => {
      FakeEventSource.latest().emit('spaces', spacesDeltaFixture);
    });

    expect(container.querySelector<HTMLElement>('[data-code="E100"]')!.dataset.state).toBe('occupied');
    expect(vi.getTimerCount()).toBe(timers);
    expect(screen.getByText('Sensores — libres: 210, ocupados: 67, fuera de servicio: 3.')).toBeInTheDocument();
  });

  test('frame to screen: a pushed availability frame updates the count without a timer (M2B-42)', () => {
    renderHome();
    expect(tile('Disponibles').getByText('211')).toBeInTheDocument();
    expect(screen.getByText('Conectando…')).toBeInTheDocument();
    const timers = vi.getTimerCount();

    act(() => {
      FakeEventSource.latest().open();
      FakeEventSource.latest().emit('availability', { ...availabilityReducedFixture, version: 121, available: 210 }, 121);
    });

    // No timer was advanced. The one new timer is the Announcer's re-check at its `dueAt` (the count changed inside its 30 s window).
    expect(tile('Disponibles').getByText('210')).toBeInTheDocument();
    expect(screen.getByText(/En tiempo real/)).toBeInTheDocument();
    expect(vi.getTimerCount()).toBe(timers + 1);
  });

  test('stale keeps the last values and shows the frame time', () => {
    stubFetch(jsonResponse(500, { error: { code: 'internal.unexpected', message: 'x', details: {} }, meta: { request_id: 'r' } }));
    renderHome();

    act(() => {
      FakeEventSource.latest().open();
      vi.setSystemTime(Date.parse('2026-11-15T14:52:00Z'));
      FakeEventSource.latest().emit('heartbeat', heartbeatFixture);
    });
    act(() => {
      FakeEventSource.latest().fail(0);
    });

    expect(tile('Disponibles').getByText('211')).toBeInTheDocument();
    expect(screen.getByText('Desactualizado — última actualización 10:52 a. m.')).toBeInTheDocument();
    expect(screen.getByText('Reconectando…')).toBeInTheDocument();
  });

  test('a null apiOrigin shows the snapshot and Live updates unavailable', () => {
    renderHome(availabilityReducedFixture, null);

    expect(tile('Disponibles').getByText('211')).toBeInTheDocument();
    expect(screen.getByText('Actualizaciones en vivo no disponibles · datos de las 10:50 a. m.')).toBeInTheDocument();
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  test('closed replaces the number with Cerrado', () => {
    renderHome(availabilityClosedFixture);

    expect(screen.queryByText('211')).not.toBeInTheDocument();
    expect(tile('Disponibles').getByText('Cerrado')).toBeInTheDocument();
    expect(screen.getAllByText('Cerrado').length).toBeGreaterThanOrEqual(2); // the tile and the pill
  });

  test('full shows Lleno and 0 of 277', () => {
    renderHome(availabilityFullFixture);

    expect(screen.getByText('Lleno')).toBeInTheDocument();
    expect(tile('Disponibles').getByText('0')).toBeInTheDocument();
    expect(tile('Disponibles').getByText('de 277 espacios')).toBeInTheDocument();
  });

  test('reduced capacity shows 3 Fuera de servicio and 280 total', () => {
    renderHome();

    expect(tile('Fuera de servicio').getByText('3')).toBeInTheDocument();
    expect(tile('Espacios totales').getByText('280')).toBeInTheDocument();
    expect(tile('Ocupados').getByText('66')).toBeInTheDocument();
  });

  test('the announcer says a status change at once and a count change at most every 30 s', () => {
    renderHome();
    const region = screen.getByRole('status');
    expect(region).toBeEmptyDOMElement();

    act(() => {
      FakeEventSource.latest().open();
      FakeEventSource.latest().emit('availability', { ...availabilityFullFixture, version: 121 }, 121);
    });
    expect(region).toHaveTextContent('El parqueo está lleno.');

    act(() => {
      vi.advanceTimersByTime(5_000);
      FakeEventSource.latest().emit('availability', { ...availabilityFullFixture, version: 122, available: 4, status: 'open' }, 122);
    });
    // The status is said at once; the count of 4 waits for the 30 s window.
    expect(region).toHaveTextContent('El parqueo está abierto.');
    expect(region).not.toHaveTextContent('Espacios disponibles');

    act(() => {
      vi.advanceTimersByTime(25_000);
    });
    expect(region).toHaveTextContent('Espacios disponibles: 4 de 277.');
  });

  test('the count is plain text and the live region is the only status element', () => {
    renderHome();
    expect(screen.getAllByRole('status')).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 2, name: 'Disponibilidad' })).toBeInTheDocument();
  });

  describe('a newer server snapshot (client-side navigation or router.refresh)', () => {
    test('replaces the cached figures and the time label with the new snapshot', () => {
      const { rerender } = renderWithProviders(homeProps(null, snapshot()), { locale: 'es-DO' });
      expect(tile('Disponibles').getByText('211')).toBeInTheDocument();

      rerender(homeProps(null, newer(150, 130, 120_000)));

      expect(tile('Disponibles').getByText('150')).toBeInTheDocument();
      expect(screen.getByText('Actualizaciones en vivo no disponibles · datos de las 10:52 a. m.')).toBeInTheDocument();
    });

    test('ignores an older snapshot', () => {
      const { rerender } = renderWithProviders(homeProps(null, newer(150, 130, 120_000)), { locale: 'es-DO' });

      rerender(homeProps(null, snapshot()));

      expect(tile('Disponibles').getByText('150')).toBeInTheDocument();
    });

    test('keeps the stream version guard: a snapshot with an older version never replaces a newer frame', () => {
      const { rerender } = renderWithProviders(homeProps(ORIGIN, snapshot()), { locale: 'es-DO' });
      act(() => {
        FakeEventSource.latest().open();
        FakeEventSource.latest().emit('availability', { ...availabilityReducedFixture, version: 140, available: 90 }, 140);
      });
      expect(tile('Disponibles').getByText('90')).toBeInTheDocument();

      rerender(homeProps(ORIGIN, newer(150, 130, 120_000)));

      expect(tile('Disponibles').getByText('90')).toBeInTheDocument();
    });
  });
});
