import { errorBody, successBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { AuthFlowProbe } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { renderWithProviders } from '@/testing/render';
import { ForgotPasswordForm } from './ForgotPasswordForm';

const F = en.auth.forgot;

function renderForgot() {
  renderWithProviders(
    <>
      <ForgotPasswordForm />
      <AuthFlowProbe />
    </>,
    { locale: 'en' },
  );
  fireEvent.change(screen.getByLabelText(en.auth.fields.email), { target: { value: 'ana@example.do' } });
  fireEvent.click(screen.getByRole('button', { name: F.submit }));
}

describe('ForgotPasswordForm', () => {
  test.each([
    ['accepted', jsonResponse(202, successBody({ status: 'accepted' }))],
    ['an unknown account', jsonResponse(404, errorBody('http.not_found'))],
  ])('shows the same sent message: %s', async (_name, response) => {
    const fetchMock = stubFetch(response);
    renderForgot();

    expect(await screen.findByRole('status')).toHaveTextContent(F.sent);
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/password/forgot');
    expect(sentBody(fetchMock)).toEqual({ email: 'ana@example.do' });
    expect(screen.getByRole('link', { name: F.haveCode })).toHaveAttribute('href', '/en/reset-password');
  });

  test('keeps the email for the reset screen and resets the form', async () => {
    stubFetch(jsonResponse(202, successBody({ status: 'accepted' })));
    renderForgot();

    await screen.findByRole('status');

    expect(screen.getByTestId('auth-flow')).toHaveTextContent(JSON.stringify({ email: 'ana@example.do' }));
    expect(screen.getByLabelText(en.auth.fields.email)).toHaveValue('');
  });

  test('validates the email before calling the API', async () => {
    const fetchMock = stubFetch(jsonResponse(202, successBody({ status: 'accepted' })));
    renderWithProviders(<ForgotPasswordForm />, { locale: 'en' });
    fireEvent.change(screen.getByLabelText(en.auth.fields.email), { target: { value: 'not-an-email' } });

    fireEvent.click(screen.getByRole('button', { name: F.submit }));

    expect(await screen.findByText('Enter a valid email address.')).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('a 429 shows the wait time and no sent message', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 15 })));
    renderForgot();

    expect(await screen.findByRole('alert')).toHaveTextContent('15');
    await waitFor(() => expect(screen.getByRole('button', { name: F.submit })).toBeDisabled());
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
