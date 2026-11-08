import { adminFixture, legalCurrentFixture, userFixture, vehicleFixture } from '@mypark/contracts/fixtures';
import { screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { describe, expect, test, vi } from 'vitest';
import { getVehicle, listVehicles } from '@/lib/api/server';
import { requireAdmin, requireDriver } from '@/lib/auth/guards';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import AdminPage from './(admin)/admin/page';
import EditVehiclePage from './(driver)/vehicles/[id]/edit/page';
import NewVehiclePage from './(driver)/vehicles/new/page';
import VehiclesPage from './(driver)/vehicles/page';
import ProfilePage from './(driver)/profile/page';

vi.mock('@/lib/auth/guards', () => ({ requireDriver: vi.fn(), requireAdmin: vi.fn() }));
vi.mock('@/lib/api/server', () => ({ listVehicles: vi.fn(), getVehicle: vi.fn() }));
vi.mock('next-intl/server', () => ({ getLocale: async () => 'en', getTranslations: async () => (key: string) => key }));
vi.mock('@/features/vehicles/VehiclesScreen', () => ({ VehiclesScreen: () => <p>vehicles screen</p> }));
vi.mock('@/features/vehicles/VehicleForm', () => ({ VehicleForm: () => <p>vehicle form</p> }));
vi.mock('@/features/profile/ProfileForm', () => ({ ProfileForm: () => <p>profile form</p> }));
vi.mock('@/features/admin/AdminLanding', () => ({ AdminLanding: () => <p>admin landing</p> }));

type Gate = Awaited<ReturnType<typeof requireDriver>>;
const NOT_READY: Array<[string, Gate]> = [
  ['legal', { state: 'legal', user: userFixture, legal: legalCurrentFixture }],
  ['suspended', { state: 'suspended' }],
  ['unavailable', { state: 'unavailable' }],
];

const ID = vehicleFixture.id;
const driverPages: Array<[string, () => Promise<ReactElement | null>, string]> = [
  ['vehicles', async () => VehiclesPage({ searchParams: Promise.resolve({}) }), 'vehicles screen'],
  ['new vehicle', async () => NewVehiclePage(), 'vehicle form'],
  ['edit vehicle', async () => EditVehiclePage({ params: Promise.resolve({ id: ID }) }), 'vehicle form'],
  ['profile', async () => ProfilePage(), 'profile form'],
];

function setup() {
  vi.mocked(listVehicles).mockResolvedValue({ items: [], page: { limit: 5, next_cursor: null } });
  vi.mocked(getVehicle).mockResolvedValue(vehicleFixture);
}

describe.each(driverPages)('driver page: %s', (_name, page, content) => {
  test('calls the driver guard with the locale, then renders when ready', async () => {
    setup();
    vi.mocked(requireDriver).mockResolvedValue({ state: 'ready', user: userFixture });

    renderWithProviders((await page()) as ReactElement, { locale: 'en' });

    expect(requireDriver).toHaveBeenCalledWith('en');
    expect(screen.getByText(content)).toBeInTheDocument();
    expect(router.refresh).not.toHaveBeenCalled();
  });

  test.each(NOT_READY)('a %s state makes no data call, renders no content and refreshes once', async (_state, gate) => {
    setup();
    vi.mocked(requireDriver).mockResolvedValue(gate);

    renderWithProviders((await page()) as ReactElement, { locale: 'en' });

    expect(listVehicles).not.toHaveBeenCalled();
    expect(getVehicle).not.toHaveBeenCalled();
    expect(screen.queryByText(content)).not.toBeInTheDocument();
    expect(router.refresh).toHaveBeenCalledTimes(1);
  });
});

describe('admin page', () => {
  test('calls the admin guard, then renders when ready', async () => {
    vi.mocked(requireAdmin).mockResolvedValue({ state: 'ready', user: adminFixture });

    renderWithProviders((await AdminPage()) as ReactElement, { locale: 'en' });

    expect(requireAdmin).toHaveBeenCalledWith('en');
    expect(screen.getByText('admin landing')).toBeInTheDocument();
  });

  test.each([['suspended', { state: 'suspended' }], ['unavailable', { state: 'unavailable' }]] as const)(
    'a %s state renders no content and refreshes once',
    async (_state, gate) => {
      vi.mocked(requireAdmin).mockResolvedValue(gate);

      renderWithProviders((await AdminPage()) as ReactElement, { locale: 'en' });

      expect(screen.queryByText('admin landing')).not.toBeInTheDocument();
      expect(router.refresh).toHaveBeenCalledTimes(1);
    },
  );

  test('a driver never reaches the content: forbidden() from the guard propagates', async () => {
    vi.mocked(requireAdmin).mockRejectedValue(new Error('forbidden'));
    await expect(AdminPage()).rejects.toThrow('forbidden');
  });
});
