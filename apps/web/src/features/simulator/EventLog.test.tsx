import type { SimulatorEvent } from '@mypark/contracts';
import { act, render, screen, within } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import type { Locale } from '@/i18n/routing';
import { createConsoleStore, type ConsoleStore } from './store';
import { SimulatorStoreContext } from '@/providers/simulator-store-provider';
import { applyEvents, initialLogState, noteRun } from './log-cursor';
import { EventLog, LOG_ANNOUNCE_INTERVAL_MS } from './EventLog';
import { simulatorStatusRunningFixture } from '@mypark/contracts/fixtures';

const L = en.simulator.log;
const MESSAGES = { en, 'es-DO': esDO } as const;
const ev = (seq: number, extra: Partial<SimulatorEvent> = {}): SimulatorEvent => ({
  seq,
  at: '2026-11-15T18:32:05Z',
  device: 'dispenser-1',
  type: 'entry.credential_presented',
  result: 'open',
  ...extra,
});

let store: ConsoleStore;
beforeEach(() => {
  vi.useFakeTimers();
  store = createConsoleStore();
});
afterEach(() => vi.useRealTimers());

function mount(locale: Locale = 'en') {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <NextIntlClientProvider locale={locale} messages={MESSAGES[locale]} timeZone="America/Santo_Domingo">
      <SimulatorStoreContext value={store}>{children}</SimulatorStoreContext>
    </NextIntlClientProvider>
  );
  return render(<EventLog />, { wrapper });
}

function push(...events: SimulatorEvent[]) {
  act(() => {
    const { log, setLog } = store.getState();
    setLog(applyEvents(log, log.cursor, events));
  });
}

describe('EventLog', () => {
  test('the empty log says No events yet. Start a scenario.', () => {
    mount();
    expect(screen.getByText(L.empty)).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  test('the table has a caption and column headers', () => {
    mount();
    push(ev(1));
    const table = screen.getByRole('table', { name: L.caption });
    const headers = within(table).getAllByRole('columnheader').map((header) => header.textContent);
    expect(headers).toEqual([L.seq, L.time, L.device, L.type, L.result, L.reason, L.car]);
    expect(table.closest('[role="region"]')).toHaveAccessibleName(en.simulator.log.title);
  });

  test('rows are newest first and show the Santo Domingo time', () => {
    mount();
    push(ev(1), ev(2, { at: '2026-11-15T18:32:06Z' }));
    const rows = screen.getAllByRole('row').slice(1);
    expect(rows[0]).toHaveTextContent('2');
    expect(rows[0]).toHaveTextContent('14:32:06');
    expect(rows[1]).toHaveTextContent('14:32:05');
  });

  test('denials carry a Denegado text badge', () => {
    mount('es-DO');
    push(ev(1, { result: 'deny', reason: 'gate.lot_full', car_id: 'car-000214' }), ev(2, { result: 'ack' }));
    const rows = screen.getAllByRole('row');
    expect(within(rows[1]!).queryByText(esDO.simulator.log.denied)).not.toBeInTheDocument();
    expect(within(rows[2]!).getByText(esDO.simulator.log.denied)).toBeInTheDocument();
  });

  test('reasons are translated when known and raw otherwise', () => {
    mount('es-DO');
    push(ev(1, { result: 'deny', reason: 'gate.lot_full' }), ev(2, { result: 'deny', reason: 'weird.code' }));
    expect(screen.getByText('El estacionamiento está lleno.')).toBeInTheDocument();
    expect(screen.getByText('gate.lot_full').tagName).toBe('CODE');
    const raw = screen.getByText('weird.code');
    expect(raw.tagName).toBe('CODE');
    expect(raw.parentElement?.tagName).toBe('TD');
  });

  test('a reason that names a group of messages, not one message, is shown raw', () => {
    mount();
    push(ev(1, { reason: 'gate' }), ev(2, { reason: 'errors' }));
    expect(screen.getByText('gate').tagName).toBe('CODE');
  });

  test('gap, run and restart rows are shown as full-width notes', () => {
    mount();
    act(() => {
      let log = applyEvents(initialLogState, 0, [ev(1)]);
      log = applyEvents(log, 1, [ev(5)]);
      log = noteRun(log, { ...simulatorStatusRunningFixture, started_at: '2026-11-15T14:30:00Z' });
      store.getState().setLog(applyEvents(log, 5, [ev(6)]));
    });
    expect(screen.getByText('Some events were skipped (2–4).')).toBeInTheDocument();
    expect(screen.getByText(/New run · Weekday · 10:30/)).toBeInTheDocument();
  });

  test('the table is not a live region', () => {
    mount();
    push(ev(1));
    const table = screen.getByRole('table');
    expect(table.closest('[role="status"],[role="alert"],[aria-live]')).toBeNull();
  });

  test('new events are announced at most every 10 s', () => {
    mount();
    const region = screen.getByRole('status');
    expect(region).toHaveTextContent('');
    push(ev(1), ev(2));
    act(() => void vi.advanceTimersByTime(0));
    expect(region).toHaveTextContent('2 new events');

    push(ev(3));
    act(() => void vi.advanceTimersByTime(LOG_ANNOUNCE_INTERVAL_MS - 1_000));
    expect(region).toHaveTextContent('2 new events');

    act(() => void vi.advanceTimersByTime(1_000));
    expect(region).toHaveTextContent('1 new event');
  });
});
