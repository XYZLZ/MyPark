import { ApiError, lotQueryKeys, type Availability, type SpaceState } from '@mypark/contracts';
import {
  availabilityReducedFixture,
  errorBody,
  listBody,
  lotSummaryFixture,
  spacesDeltaFixture,
  spacesFixture,
  spacesFullEventFixture,
  sseText,
} from '@mypark/contracts/fixtures';
import { QueryClient, skipToken, useQuery } from '@tanstack/react-query';
import { act, cleanup, screen } from '@testing-library/react-native';
import { Text } from 'react-native';

import { getApiBaseUrl } from '@/lib/env';
import { http } from '@/services/runtime';
import { notifyQuerySynchronously } from '@/testing/query';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';
import { controllableStream, type ControllableStream } from '@/testing/stream';

import { acquireLiveLot, resetLiveLotsForTests } from './manager';
import { useLiveLot } from './useLiveLot';
import { useLotId } from './useLotId';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const LOT = 'L1';

/** Reads what the live block reads: the connection, the counter and one map cell from the Query cache. */
function Probe() {
  const connection = useLiveLot(LOT);
  const { data: availability } = useQuery<Availability>({ queryKey: lotQueryKeys.availability(LOT), queryFn: skipToken });
  const { data: spaces } = useQuery<SpaceState[]>({ queryKey: lotQueryKeys.spaces(LOT), queryFn: skipToken });
  return (
    <>
      <Text testID="count">{availability?.available}</Text>
      <Text testID="status">{connection.status}</Text>
      <Text testID="E100">{spaces?.find((s) => s.code === 'E100')?.state}</Text>
    </>
  );
}

describe('useLiveLot', () => {
  let queryClient: QueryClient;
  let stream: ControllableStream;
  let restoreNotify: () => void;
  let releaseDriver: () => void;
  /** The REST fallback stays pending until a test settles it. */
  let rejectSnapshot: (error: unknown) => void;

  beforeEach(() => {
    jest.useFakeTimers();
    restoreNotify = notifyQuerySynchronously();
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
    queryClient.setQueryData(lotQueryKeys.availability(LOT), { ...availabilityReducedFixture, version: 120 });
    queryClient.setQueryData(lotQueryKeys.spaces(LOT), spacesFixture.spaces);
    stream = controllableStream();
    // The first acquirer's deps drive the shared connection; the hook then joins it.
    releaseDriver = acquireLiveLot(getApiBaseUrl(), LOT, queryClient, {
      fetchImpl: stream.fetchImpl,
      random: () => 1,
      fetchSnapshot: () => new Promise((_resolve, reject) => (rejectSnapshot = reject)),
    }).release;
  });

  afterEach(async () => {
    await cleanup();
    releaseDriver();
    resetLiveLotsForTests();
    restoreNotify();
    jest.useRealTimers();
  });

  test('frame to screen: a pushed availability frame updates the screen with no timer advanced (M2B-42)', async () => {
    await renderScreen(<Probe />, { queryClient });
    expect(screen.getByTestId('count')).toHaveTextContent('211');
    expect(screen.getByTestId('status')).toHaveTextContent('connecting');

    await act(async () => stream.push(sseText('availability', { ...availabilityReducedFixture, version: 121, available: 210 })));

    expect(screen.getByTestId('count')).toHaveTextContent('210');
    expect(screen.getByTestId('status')).toHaveTextContent('live');
  });

  test('frame to screen: a spaces delta changes the cell state with no timer advanced (M2B-42)', async () => {
    await renderScreen(<Probe />, { queryClient });
    await act(async () => stream.push(sseText('spaces', spacesFullEventFixture)));
    expect(screen.getByTestId('E100')).toHaveTextContent('free');

    await act(async () => stream.push(sseText('spaces', spacesDeltaFixture)));

    expect(screen.getByTestId('E100')).toHaveTextContent('occupied');
  });

  test('lot.not_found invalidates the lot list once', async () => {
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');
    await renderScreen(<Probe />, { queryClient });

    await act(async () => stream.respond(404, JSON.stringify(errorBody('lot.not_found'))));

    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: lotQueryKeys.all });
  });

  test('lot.not_found from the stream and then from the REST snapshot invalidates once', async () => {
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');
    await renderScreen(<Probe />, { queryClient });
    await act(async () => stream.respond(503, JSON.stringify(errorBody('internal.unavailable'))));
    const rejectFirstSnapshot = rejectSnapshot;
    await act(async () => {
      await jest.advanceTimersByTimeAsync(10_000);
      stream.respond(404, JSON.stringify(errorBody('lot.not_found')));
    });
    await act(async () => rejectFirstSnapshot(new ApiError({ code: 'lot.not_found', message: 'gone', status: 404 })));

    expect(invalidate).toHaveBeenCalledTimes(1);
  });
});

function LotIdProbe() {
  const { lotId, status } = useLotId();
  return <Text testID="lot">{`${status}:${lotId ?? 'none'}`}</Text>;
}

describe('useLotId', () => {
  const request = jest.mocked(http.request);

  beforeEach(() => request.mockReset());

  test('resolves the first lot of the list', async () => {
    request.mockResolvedValue(reply(200, listBody([lotSummaryFixture, { ...lotSummaryFixture, id: 'second' }], 20)));
    await renderScreen(<LotIdProbe />);
    expect(await screen.findByText(`success:${lotSummaryFixture.id}`)).toBeOnTheScreen();
    expect(request).toHaveBeenCalledWith({ method: 'GET', path: '/v1/lots' });
  });

  test('an empty list gives no lot', async () => {
    request.mockResolvedValue(reply(200, listBody([], 20)));
    await renderScreen(<LotIdProbe />);
    expect(await screen.findByText('success:none')).toBeOnTheScreen();
  });
});
