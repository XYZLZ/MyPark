import { errorBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import { LOGOUT_ALL_UNCONFIRMED, LOGOUT_ALL_UNCONFIRMED_STATUS } from '@/lib/bff/logout-all';
import { hardNavigate } from '@/lib/browser/navigate';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { renderWithProviders } from '@/testing/render';
import { LogoutButtons } from './LogoutButtons';

vi.mock('@/lib/browser/navigate', () => ({ hardNavigate: vi.fn() }));

describe('LogoutButtons', () => {
  test.each([
    ['this', en.session.logout, '/api/v1/auth/logout'],
    ['both', en.session.logoutAll, '/api/v1/auth/logout-all'],
  ] as const)('scope %s posts to the matching route and goes home', async (scope, name, path) => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderWithProviders(<LogoutButtons scope={scope} />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en'));
    expect(fetchMock.mock.calls[0]![0]).toBe(path);
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'POST' });
  });

  test.each([401, 403, 500])('any response status (%s) counts as done', async (status) => {
    stubFetch(jsonResponse(status, errorBody(status === 403 ? 'auth.account_suspended' : 'internal.unexpected')));
    renderWithProviders(<LogoutButtons scope="this" />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: en.session.logout }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en'));
  });

  test('logout on all devices that the BFF reports as unconfirmed goes home with the notice', async () => {
    stubFetch(jsonResponse(LOGOUT_ALL_UNCONFIRMED_STATUS, errorBody(LOGOUT_ALL_UNCONFIRMED)));
    renderWithProviders(<LogoutButtons scope="both" />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: en.session.logoutAll }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en?notice=logout_all_failed'));
  });

  test('logout on all devices without a 2xx is never taken as confirmed', async () => {
    stubFetch(jsonResponse(500, errorBody('internal.unexpected')));
    renderWithProviders(<LogoutButtons scope="both" />, { locale: 'es-DO' });

    fireEvent.click(screen.getByRole('button', { name: esDO.session.logoutAll }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/es-DO?notice=logout_all_failed'));
  });

  test('the BFF CSRF denial (403 auth.forbidden) is not a logout: show an error and stay', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.forbidden')));
    renderWithProviders(<LogoutButtons scope="this" />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: en.session.logout }));

    expect(await screen.findByRole('alert')).toHaveTextContent(en.errors.auth.forbidden);
    expect(hardNavigate).not.toHaveBeenCalled();
    // The button is usable again.
    expect(screen.getByRole('button', { name: en.session.logout })).toBeEnabled();
  });

  test('a double click sends one request', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderWithProviders(<LogoutButtons scope="this" />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: en.session.logout }));
    fireEvent.click(screen.getByRole('button', { name: en.session.loggingOut }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledTimes(1));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test('a network failure stays here and says so', async () => {
    stubFetch(() => Promise.reject(new TypeError('offline')));
    renderWithProviders(<LogoutButtons scope="this" />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: en.session.logout }));

    expect(await screen.findByRole('alert')).toHaveTextContent(en.errors.client.network);
    expect(hardNavigate).not.toHaveBeenCalled();
  });
});
