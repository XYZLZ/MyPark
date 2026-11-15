import { lotQueryKeys, type Availability } from '@mypark/contracts';
import { availabilityReducedFixture, errorBody } from '@mypark/contracts/fixtures';
import { useQuery } from '@tanstack/react-query';
import { act, cleanup, screen } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { FakeEventSource } from '@/testing/event-source';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { notifyQuerySynchronously } from '@/testing/query';
import { renderWithProviders } from '@/testing/render';
import { resetLiveLotsForTests } from './manager';
import { useLiveLot } from './useLiveLot';

const ORIGIN = 'http://api.test';
const LOT = 'L1';

let restoreNotify: () => void;

beforeEach(() => {
  vi.useFakeTimers();
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

/** Reads the count the way the live block does: the Query cache seeded with the RSC snapshot. */
function Probe({ origin }: { origin: string | null }) {
  const connection = useLiveLot(LOT, origin);
  const { data } = useQuery<Availability>({
    queryKey: lotQueryKeys.availability(LOT),
    queryFn: () => Promise.reject(new Error('the stream and the RSC fill this key')),
    initialData: availabilityReducedFixture,
    staleTime: Infinity,
  });
  return (
    <p>
      <span data-testid="count">{data.available}</span> <span data-testid="status">{connection.status}</span>
    </p>
  );
}

test('a null origin never opens a connection', () => {
  renderWithProviders(<Probe origin={null} />);
  expect(screen.getByTestId('status')).toHaveTextContent('unavailable');
  vi.advanceTimersByTime(60_000);
  expect(FakeEventSource.instances).toHaveLength(0);
});

test('StrictMode double mount reuses one connection', () => {
  const { unmount } = renderWithProviders(
    <StrictMode>
      <Probe origin={ORIGIN} />
    </StrictMode>,
  );
  vi.advanceTimersByTime(5_000);
  expect(FakeEventSource.instances).toHaveLength(1);
  expect(FakeEventSource.latest().closed).toBe(false);
  unmount();
  vi.advanceTimersByTime(2_000);
  expect(FakeEventSource.latest().closed).toBe(true);
});

test('frame to screen: a pushed availability frame updates the DOM with no timer advanced (M2B-42)', () => {
  renderWithProviders(<Probe origin={ORIGIN} />);
  expect(screen.getByTestId('count')).toHaveTextContent('211');
  expect(screen.getByTestId('status')).toHaveTextContent('connecting');
  const timers = vi.getTimerCount();
  act(() => {
    FakeEventSource.latest().open();
    FakeEventSource.latest().emit('availability', { ...availabilityReducedFixture, version: 121, available: 210 }, 121);
  });
  expect(screen.getByTestId('count')).toHaveTextContent('210');
  expect(screen.getByTestId('status')).toHaveTextContent('live');
  expect(vi.getTimerCount()).toBe(timers);
});

test('a lot.not_found snapshot refreshes the route once', async () => {
  stubFetch(jsonResponse(404, errorBody('lot.not_found')));
  renderWithProviders(<Probe origin={ORIGIN} />);
  await act(async () => {
    FakeEventSource.latest().fail(2);
    await vi.advanceTimersByTimeAsync(60_000);
  });
  expect(router.refresh).toHaveBeenCalledTimes(1);
  expect(FakeEventSource.instances).toHaveLength(1);
});
