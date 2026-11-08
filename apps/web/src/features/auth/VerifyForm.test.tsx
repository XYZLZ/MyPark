import { errorBody, successBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { AuthFlowProbe, SeedAuthFlow } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { VerifyForm } from './VerifyForm';

const L = en.auth.fields;
const V = en.auth.verify;

function renderVerify(email = 'ana@example.do') {
  return renderWithProviders(
    <>
      <SeedAuthFlow email={email}>
        <VerifyForm />
      </SeedAuthFlow>
      <AuthFlowProbe />
    </>,
    { locale: 'en' },
  );
}

const fillCode = (value = '123456') => fireEvent.change(screen.getByLabelText(L.code), { target: { value } });
const submit = () => fireEvent.click(screen.getByRole('button', { name: V.submit }));
const sentOk = () => jsonResponse(202, successBody({ status: 'accepted' }));

describe('VerifyForm', () => {
  test('pre-fills the email from authFlow and uses one-time-code', async () => {
    renderVerify();

    expect(await screen.findByLabelText(L.email)).toHaveValue('ana@example.do');
    const code = screen.getByLabelText(L.code);
    expect(code).toHaveAttribute('autocomplete', 'one-time-code');
    expect(code).toHaveAttribute('inputmode', 'numeric');
  });

  test('validates the code before calling the API', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderVerify();
    await screen.findByLabelText(L.email);
    fillCode('12');

    submit();

    expect(await screen.findByText('The code has 6 digits.')).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('code_invalid is a field error with a resend offer', async () => {
    stubFetch(jsonResponse(422, errorBody('auth.code_invalid')));
    renderVerify();
    await screen.findByLabelText(L.email);
    fillCode();

    submit();

    await waitFor(() => expect(screen.getByLabelText(L.code)).toBeInvalid());
    expect(screen.getByLabelText(L.code)).toHaveAccessibleDescription('The code is not valid or has expired.');
    expect(screen.getByLabelText(L.code)).toHaveFocus();
    expect(screen.getByRole('button', { name: V.resend })).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  test('verified goes to login, email kept, password empty', async () => {
    const fetchMock = stubFetch(jsonResponse(204));
    renderVerify();
    await screen.findByLabelText(L.email);
    fillCode();

    submit();

    await waitFor(() => expect(router.push).toHaveBeenCalledExactlyOnceWith('/login'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/verify');
    expect(sentBody(fetchMock)).toEqual({ email: 'ana@example.do', code: '123456' });
    expect(screen.getByTestId('auth-flow')).toHaveTextContent(JSON.stringify({ email: 'ana@example.do' }));
    expect(screen.getByTestId('auth-flow').textContent).not.toMatch(/password/i);
  });

  test.each([
    ['accepted', sentOk()],
    ['an unknown account', jsonResponse(404, errorBody('http.not_found'))],
    ['a conflict', jsonResponse(409, errorBody('user.email_taken'))],
  ])('resend shows the same sent message on every outcome: %s', async (_name, response) => {
    const fetchMock = stubFetch(response);
    renderVerify();
    await screen.findByLabelText(L.email);

    fireEvent.click(screen.getByRole('button', { name: V.resend }));

    expect(await screen.findByRole('status')).toHaveTextContent(V.sent);
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/verify/resend');
    expect(sentBody(fetchMock)).toEqual({ email: 'ana@example.do' });
  });

  test('resend without a valid email asks for it instead of calling the API', async () => {
    const fetchMock = stubFetch(sentOk());
    renderVerify('');
    await screen.findByLabelText(L.email);

    fireEvent.click(screen.getByRole('button', { name: V.resend }));

    expect(await screen.findByText('This field is required.')).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('resend on 429 shows the wait time instead of the sent message', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 30 })));
    renderVerify();
    await screen.findByLabelText(L.email);

    fireEvent.click(screen.getByRole('button', { name: V.resend }));

    expect(await screen.findByRole('alert')).toHaveTextContent('30');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: V.resend })).toBeDisabled();
  });
});
