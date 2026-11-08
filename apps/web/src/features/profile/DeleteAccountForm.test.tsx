import { errorBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { hardNavigate } from '@/lib/browser/navigate';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { DeleteAccountForm } from './DeleteAccountForm';

vi.mock('@/lib/browser/navigate', () => ({ hardNavigate: vi.fn() }));

const PASSWORD = 'Sup3rSecret!!';
const field = () => screen.getByLabelText(en.auth.fields.password);
const submit = () => fireEvent.click(screen.getByRole('button', { name: en.profile.danger.submit }));

describe('DeleteAccountForm', () => {
  test('explains consequences and requires the password', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderWithProviders(<DeleteAccountForm />, { locale: 'en' });

    expect(screen.getByText(en.profile.danger.body)).toBeInTheDocument();
    submit();

    expect(await screen.findByText(en.validation.required)).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('password_incorrect lands on password and never refreshes', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.password_incorrect')));
    renderWithProviders(<DeleteAccountForm />, { locale: 'en' });

    fireEvent.change(field(), { target: { value: PASSWORD } });
    submit();

    expect(await screen.findByText(en.errors.auth.password_incorrect)).toBeInTheDocument();
    expect(field()).toHaveAttribute('aria-invalid', 'true');
    expect(router.refresh).not.toHaveBeenCalled();
    expect(router.push).not.toHaveBeenCalled();
    expect(hardNavigate).not.toHaveBeenCalled();
  });

  test('success returns to the home page', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderWithProviders(<DeleteAccountForm />, { locale: 'en' });

    fireEvent.change(field(), { target: { value: PASSWORD } });
    submit();

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith('/en'));
    expect(sentBody(fetchMock)).toEqual({ password: PASSWORD });
  });

  test('the password field resets on success', async () => {
    stubFetch(jsonResponse(204));
    renderWithProviders(<DeleteAccountForm />, { locale: 'en' });

    fireEvent.change(field(), { target: { value: PASSWORD } });
    submit();

    await waitFor(() => expect(hardNavigate).toHaveBeenCalled());
    expect(field()).toHaveValue('');
  });
});
