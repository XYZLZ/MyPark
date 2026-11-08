import { errorBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { AuthFlowProbe } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { AcceptInvitationForm } from './AcceptInvitationForm';

const F = en.auth.fields;
const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });

function fillAll() {
  fill(F.email, 'admin@example.do');
  fill(F.code, '123456');
  fill(F.password, 'Sup3rSecret!!');
  fireEvent.click(screen.getByRole('button', { name: en.acceptInvitation.submit }));
}

const renderForm = () =>
  renderWithProviders(
    <>
      <AcceptInvitationForm />
      <AuthFlowProbe />
    </>,
    { locale: 'en' },
  );

describe('AcceptInvitationForm', () => {
  test('success goes to admin login with the email kept', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderForm();

    fillAll();

    await waitFor(() => expect(router.push).toHaveBeenCalledWith('/admin/login'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/invitations/accept');
    expect(sentBody(fetchMock)).toEqual({ email: 'admin@example.do', code: '123456', password: 'Sup3rSecret!!' });
    expect(screen.getByTestId('auth-flow')).toHaveTextContent('admin@example.do');
    expect(screen.getByTestId('auth-flow')).not.toHaveTextContent('Sup3rSecret');
    expect(screen.getByLabelText(F.password)).toHaveValue('');
  });

  test('password_common lands on password', async () => {
    stubFetch(jsonResponse(422, errorBody('validation.failed', { fields: [{ field: 'password', rule: 'password_common' }] })));
    renderForm();

    fillAll();

    expect(await screen.findByText(en.validation.password_common)).toBeInTheDocument();
    expect(screen.getByLabelText(F.password)).toHaveAttribute('aria-invalid', 'true');
  });

  test('code_invalid lands on code', async () => {
    stubFetch(jsonResponse(422, errorBody('auth.code_invalid')));
    renderForm();

    fillAll();

    expect(await screen.findByText(en.errors.auth.code_invalid)).toBeInTheDocument();
    expect(screen.getByLabelText(F.code)).toHaveAttribute('aria-invalid', 'true');
  });

  test('a malformed code is rejected on the client before any request', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderForm();

    fill(F.email, 'admin@example.do');
    fill(F.code, '12ab');
    fill(F.password, 'Sup3rSecret!!');
    fireEvent.click(screen.getByRole('button', { name: en.acceptInvitation.submit }));

    expect(await screen.findByText(en.validation.code6)).toBeInTheDocument();
    expect(screen.getByLabelText(F.code)).toHaveAttribute('aria-invalid', 'true');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('429 shows the wait time and locks submit', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 42 })));
    renderForm();

    fillAll();

    expect(await screen.findByRole('alert')).toHaveTextContent('Try again in 42 s.');
    expect(screen.getByRole('button', { name: en.acceptInvitation.submit })).toBeDisabled();
    expect(router.push).not.toHaveBeenCalled();
  });
});
