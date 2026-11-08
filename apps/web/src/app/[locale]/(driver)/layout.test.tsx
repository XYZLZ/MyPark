import { legalCurrentFixture, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen } from '@testing-library/react';
import { createTranslator } from 'next-intl';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { requireDriver } from '@/lib/auth/guards';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { renderWithProviders } from '@/testing/render';
import DriverLayout from './layout';

vi.mock('@/lib/auth/guards', () => ({ requireDriver: vi.fn() }));
vi.mock('next-intl/server', () => ({
  getTranslations: async (namespace: string) => createTranslator({ locale: 'en', messages: en, namespace: namespace as 'common' }),
}));

async function renderLayout(gate: Awaited<ReturnType<typeof requireDriver>>) {
  vi.mocked(requireDriver).mockResolvedValue(gate);
  const tree = await DriverLayout({ children: <p>vehicle list</p>, params: Promise.resolve({ locale: 'en' }) });
  return renderWithProviders(tree, { locale: 'en' });
}

describe('DriverLayout', () => {
  test('legal state renders LegalGate instead of children', async () => {
    await renderLayout({ state: 'legal', user: userFixture, legal: legalCurrentFixture });

    expect(screen.getByRole('heading', { name: en.legalGate.title })).toBeInTheDocument();
    expect(screen.queryByText('vehicle list')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: en.common.nav.vehicles })).not.toBeInTheDocument();
  });

  test('suspended state renders AccountBlocked with Log out instead of children', async () => {
    await renderLayout({ state: 'suspended' });

    expect(screen.getByRole('heading', { name: en.accountBlocked.title })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.session.logout })).toBeInTheDocument();
    expect(screen.queryByText('vehicle list')).not.toBeInTheDocument();
  });

  test('unavailable state renders the session-unavailable panel, not a logout', async () => {
    await renderLayout({ state: 'unavailable' });

    expect(screen.getByRole('heading', { name: en.session.unavailable.title })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.session.unavailable.retry })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: en.session.logout })).not.toBeInTheDocument();
    expect(screen.queryByText('vehicle list')).not.toBeInTheDocument();
  });

  test('ready state renders children with the Vehicles and Profile nav', async () => {
    await renderLayout({ state: 'ready', user: userFixture });

    expect(screen.getByText('vehicle list')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: en.common.nav.vehicles })).toHaveAttribute('href', '/en/vehicles');
    expect(screen.getByRole('link', { name: en.common.nav.profile })).toHaveAttribute('href', '/en/profile');
  });

  test('language switch behind the gate does not call PATCH, but a ready session does', async () => {
    const fetchMock = stubFetch(jsonResponse(200, { data: userFixture, meta: { request_id: '01HZX3V8Q2W9K7M4N5P6R7S8T9' } }));

    const gated = await renderLayout({ state: 'legal', user: userFixture, legal: legalCurrentFixture });
    fireEvent.click(screen.getByRole('link', { name: 'Español' }));
    expect(fetchMock).not.toHaveBeenCalled();
    gated.unmount();

    await renderLayout({ state: 'ready', user: userFixture });
    fireEvent.click(screen.getByRole('link', { name: 'Español' }));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/me');
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'PATCH', body: JSON.stringify({ locale: 'es-DO' }) });
  });
});
