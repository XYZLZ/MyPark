import { errorBody, successBody, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { ProfileForm } from './ProfileForm';

const F = en.auth.fields;
const save = () => fireEvent.click(screen.getByRole('button', { name: en.profile.save }));
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const renderProfile = () => renderWithProviders(<ProfileForm user={userFixture} />, { locale: 'en' });

describe('ProfileForm', () => {
  test('email is read-only', () => {
    renderProfile();
    const email = screen.getByLabelText(F.email);
    expect(email).toHaveValue(userFixture.email);
    expect(email).toHaveAttribute('readonly');
  });

  test('patches name and phone; +1 alone clears the phone', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody({ ...userFixture, first_name: 'Maria', phone: null })));
    renderProfile();

    change(F.firstName, 'Maria');
    change(F.phone, '+1  ');
    save();

    expect(await screen.findByText(en.profile.saved)).toBeInTheDocument();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/me');
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'PATCH' });
    // Only what changed, and an empty phone is what clears it.
    expect(sentBody(fetchMock)).toEqual({ first_name: 'Maria', phone: '' });
    expect(router.refresh).toHaveBeenCalled();
  });

  test('sends nothing when nothing changed', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(userFixture)));
    renderProfile();

    save();

    expect(await screen.findByText(en.profile.saved)).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('a server validation error lands under the field', async () => {
    stubFetch(jsonResponse(422, errorBody('validation.failed', { fields: [{ field: 'phone', rule: 'phone' }] })));
    renderProfile();

    change(F.phone, '+18095550000');
    save();

    await waitFor(() => expect(screen.getByLabelText(F.phone)).toHaveAttribute('aria-invalid', 'true'));
    expect(screen.getByText(en.validation.phone)).toBeInTheDocument();
  });

  test('links to forgot password', () => {
    renderProfile();
    expect(screen.getByRole('link', { name: en.profile.password.link })).toHaveAttribute('href', '/en/forgot-password');
  });
});
