import { availabilityReducedFixture, lotFixture, spacesFixture, tariffFixture } from '@mypark/contracts/fixtures';
import { screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { resetLiveLotsForTests } from '@/features/lot/live/manager';
import { FakeEventSource } from '@/testing/event-source';
import { renderWithProviders } from '@/testing/render';
import { LotSection } from './LotSection';
import { loadLotSnapshot } from './server';

vi.mock('./server', () => ({ loadLotSnapshot: vi.fn() }));
vi.mock('@/lib/env/public-api', () => ({ readPublicApiOrigin: () => 'http://api.test' }));

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
  resetLiveLotsForTests();
  FakeEventSource.instances = [];
});

describe('LotSection', () => {
  test('renders the live block with the snapshot and the public origin', async () => {
    vi.mocked(loadLotSnapshot).mockResolvedValue({
      lot: lotFixture,
      availability: availabilityReducedFixture,
      spaces: spacesFixture.spaces,
      tariff: tariffFixture,
      fetchedAt: Date.parse('2026-11-15T14:50:00Z'),
    });
    renderWithProviders(await LotSection({ variant: 'home' }), { locale: 'es-DO' });

    expect(screen.getByText('211')).toBeInTheDocument();
    expect(FakeEventSource.latest().url).toContain('http://api.test');
  });

  test('renders the error panel when there is no snapshot', async () => {
    vi.mocked(loadLotSnapshot).mockResolvedValue(null);
    renderWithProviders(await LotSection({ variant: 'home' }), { locale: 'es-DO' });

    expect(screen.getByText('La disponibilidad no está cargando ahora')).toBeInTheDocument();
    expect(FakeEventSource.instances).toHaveLength(0);
  });
});
