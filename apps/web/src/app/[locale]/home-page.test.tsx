import { screen } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { renderWithProviders } from '@/testing/render';
import HomePage from './(public)/page';

const state = { cookies: new Set<string>() };

vi.mock('next/headers', () => ({ cookies: async () => ({ has: (name: string) => state.cookies.has(name) }) }));
// An async server component cannot render in jsdom; its own tests cover it.
vi.mock('@/features/lot/LotSection', () => ({ LotSection: () => <p>lot section</p> }));

beforeEach(() => {
  state.cookies = new Set();
});

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

  test('puts the lot section in the slot', async () => {
    await render({});

    expect(await screen.findByText('lot section')).toBeInTheDocument();
  });

  test.each([['mp_access'], ['mp_refresh']])('a %s cookie shows the Vehicles link instead of Log in', async (cookie) => {
    state.cookies = new Set([cookie]);
    await render({});

    expect(screen.getByRole('link', { name: 'Vehicles' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Log in' })).not.toBeInTheDocument();
  });

  test('no session cookie shows Log in and Sign up', async () => {
    await render({});

    expect(screen.getByRole('link', { name: 'Log in' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign up' })).toBeInTheDocument();
  });
});
