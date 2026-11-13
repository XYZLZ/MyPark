import { screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { renderWithProviders } from '@/testing/render';
import HomePage from './(public)/page';

vi.mock('next/headers', () => ({ cookies: async () => ({ has: () => false }) }));

const render = async (searchParams: Record<string, string | string[] | undefined>) =>
  renderWithProviders(await HomePage({ searchParams: Promise.resolve(searchParams) }), { locale: 'en' });

describe('home page', () => {
  test('passes the logout_all_failed notice through', async () => {
    await render({ notice: 'logout_all_failed' });

    expect(screen.getByRole('status')).toHaveTextContent(en.session.logoutAllFailed);
  });

  test.each([[{}], [{ notice: 'anything_else' }], [{ notice: ['logout_all_failed', 'x'] }]])(
    'ignores any other notice (%o)',
    async (searchParams) => {
      await render(searchParams);

      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    },
  );
});
