import { adminFixture } from '@mypark/contracts/fixtures';
import { screen } from '@testing-library/react';
import { createTranslator } from 'next-intl';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { requireAdmin } from '@/lib/auth/guards';
import { renderWithProviders } from '@/testing/render';
import AdminLayout from './layout';

vi.mock('@/lib/auth/guards', () => ({ requireAdmin: vi.fn() }));
vi.mock('next-intl/server', () => ({
  getTranslations: async (namespace: string) => createTranslator({ locale: 'en', messages: en, namespace: namespace as 'common' }),
}));

const props = { children: <p>admin content</p>, params: Promise.resolve({ locale: 'en' }) };

describe('AdminLayout', () => {
  test('a driver gets forbidden() and children never render', async () => {
    // requireAdmin calls forbidden() for a driver; the layout must let it propagate.
    vi.mocked(requireAdmin).mockRejectedValue(new Error('NEXT_HTTP_ERROR_FALLBACK;403'));

    await expect(AdminLayout(props)).rejects.toThrow('NEXT_HTTP_ERROR_FALLBACK;403');
    expect(requireAdmin).toHaveBeenCalledWith('en');
  });

  test('ready renders children', async () => {
    vi.mocked(requireAdmin).mockResolvedValue({ state: 'ready', user: adminFixture });
    renderWithProviders(await AdminLayout(props), { locale: 'en' });

    expect(screen.getByText('admin content')).toBeInTheDocument();
  });

  test('suspended renders the blocked panel with Log out, not children', async () => {
    vi.mocked(requireAdmin).mockResolvedValue({ state: 'suspended' });
    renderWithProviders(await AdminLayout(props), { locale: 'en' });

    expect(screen.getByRole('heading', { name: en.accountBlocked.title })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.session.logout })).toBeInTheDocument();
    expect(screen.queryByText('admin content')).not.toBeInTheDocument();
  });

  test('unavailable renders the retry panel, not a logout and not children', async () => {
    vi.mocked(requireAdmin).mockResolvedValue({ state: 'unavailable' });
    renderWithProviders(await AdminLayout(props), { locale: 'en' });

    expect(screen.getByRole('heading', { name: en.session.unavailable.title })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: en.session.logout })).not.toBeInTheDocument();
    expect(screen.queryByText('admin content')).not.toBeInTheDocument();
  });
});
