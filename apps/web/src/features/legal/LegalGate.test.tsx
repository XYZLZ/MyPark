import { errorBody, legalCurrentFixture, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { hardNavigate } from '@/lib/browser/navigate';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { LegalGate } from './LegalGate';

vi.mock('@/lib/browser/navigate', () => ({ hardNavigate: vi.fn() }));

const renderGate = () => renderWithProviders(<LegalGate legal={legalCurrentFixture} user={userFixture} />, { locale: 'en' });

describe('LegalGate', () => {
  test('blocks with accept, log out and delete, and no app content', () => {
    renderGate();

    expect(screen.getByRole('heading', { name: en.legalGate.title })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.legalGate.accept })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.session.logout })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: en.profile.danger.submit })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: en.legalGate.terms })).toHaveAttribute('href', '/en/legal/terms');
    expect(screen.getByRole('link', { name: en.legalGate.privacy })).toHaveAttribute('href', '/en/legal/privacy');
    expect(screen.queryByRole('link', { name: en.common.nav.vehicles })).not.toBeInTheDocument();
  });

  test('accept sends the current versions and refreshes', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderGate();

    fireEvent.click(screen.getByRole('button', { name: en.legalGate.accept }));

    await waitFor(() => expect(router.refresh).toHaveBeenCalledTimes(1));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/me/legal-acceptance');
    expect(sentBody(fetchMock)).toEqual({
      terms_version: legalCurrentFixture.terms.version,
      privacy_version: legalCurrentFixture.privacy.version,
    });
  });

  test('version_outdated shows a banner and re-reads', async () => {
    stubFetch(jsonResponse(409, errorBody('legal.version_outdated')));
    renderGate();

    fireEvent.click(screen.getByRole('button', { name: en.legalGate.accept }));

    expect(await screen.findByRole('alert')).toHaveTextContent(en.errors.legal.version_outdated);
    expect(router.refresh).toHaveBeenCalledTimes(1);
  });

  test('logout works behind the gate', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderGate();

    fireEvent.click(screen.getByRole('button', { name: en.session.logout }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/logout');
  });

  test('delete works behind the gate', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderGate();

    fireEvent.change(screen.getByLabelText(en.auth.fields.password), { target: { value: 'Sup3rSecret!!' } });
    fireEvent.click(screen.getByRole('button', { name: en.profile.danger.submit }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/me');
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'DELETE' });
  });
});
