import { errorBody, legalCurrentFixture, successBody, validationBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import { AuthFlowProbe } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { RegisterForm } from './RegisterForm';

const PASSWORD = 'Sup3rSecret!!';
const accepted = () => jsonResponse(202, successBody({ status: 'verification_pending' }));

type Labels = typeof en.auth.fields;

function renderRegister(locale: 'en' | 'es-DO' = 'en') {
  const labels: Labels = locale === 'en' ? en.auth.fields : esDO.auth.fields;
  const submitName = locale === 'en' ? en.auth.register.submit : esDO.auth.register.submit;
  const utils = renderWithProviders(
    <>
      <RegisterForm legal={legalCurrentFixture} />
      <AuthFlowProbe />
    </>,
    { locale },
  );
  const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
  const fillAll = () => {
    fill(labels.firstName, 'Ana');
    fill(labels.lastName, 'Perez');
    fill(labels.email, 'ana@example.do');
    fill(labels.password, PASSWORD);
  };
  const accept = () => fireEvent.click(screen.getByRole('checkbox'));
  const submit = () => fireEvent.click(screen.getByRole('button', { name: submitName }));
  return { ...utils, labels, fill, fillAll, accept, submit };
}

describe('RegisterForm', () => {
  test('sends legal versions, locale and an empty phone for +1', async () => {
    const fetchMock = stubFetch(accepted());
    const { fillAll, accept, submit, labels } = renderRegister();
    expect(screen.getByLabelText(labels.phone)).toHaveValue('+1');
    fillAll();
    accept();

    submit();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/register');
    const body = sentBody(fetchMock);
    expect(body).toEqual({
      email: 'ana@example.do',
      password: PASSWORD,
      first_name: 'Ana',
      last_name: 'Perez',
      locale: 'en',
      accepted_terms_version: legalCurrentFixture.terms.version,
      accepted_privacy_version: legalCurrentFixture.privacy.version,
    });
    expect(body).not.toHaveProperty('phone');
  });

  test('sends a normalized phone when one is typed', async () => {
    const fetchMock = stubFetch(accepted());
    const { fillAll, accept, submit, fill, labels } = renderRegister();
    fillAll();
    fill(labels.phone, '+1 809-555-1234');
    accept();

    submit();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(sentBody(fetchMock).phone).toBe('+18095551234');
  });

  test('uses the right autocomplete values', () => {
    const { labels } = renderRegister();

    expect(screen.getByLabelText(labels.email)).toHaveAttribute('autocomplete', 'email');
    expect(screen.getByLabelText(labels.password)).toHaveAttribute('autocomplete', 'new-password');
  });

  test('requires the terms checkbox', async () => {
    const fetchMock = stubFetch(accepted());
    const { fillAll, submit } = renderRegister();
    fillAll();

    submit();

    const checkbox = screen.getByRole('checkbox');
    await waitFor(() => expect(checkbox).toBeInvalid());
    expect(checkbox).toHaveAccessibleDescription('This field is required.');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('links to both legal documents', () => {
    renderRegister();

    expect(screen.getByRole('link', { name: 'terms' })).toHaveAttribute('href', '/en/legal/terms');
    expect(screen.getByRole('link', { name: 'privacy policy' })).toHaveAttribute('href', '/en/legal/privacy');
  });

  test('field error mapping includes password_common', async () => {
    stubFetch(jsonResponse(422, validationBody([{ field: 'password', rule: 'password_common' }])));
    const { fillAll, accept, submit, labels } = renderRegister('es-DO');
    fillAll();
    accept();

    submit();

    expect(await screen.findByText('Esa contraseña es muy común. Elige otra.')).toBeInTheDocument();
    expect(screen.getByLabelText(labels.password)).toBeInvalid();
    expect(screen.getByLabelText(labels.password)).toHaveFocus();
  });

  test('email_taken shows a banner', async () => {
    stubFetch(jsonResponse(409, errorBody('user.email_taken')));
    const { fillAll, accept, submit } = renderRegister();
    fillAll();
    accept();

    submit();

    expect(await screen.findByRole('alert')).toHaveTextContent('We could not complete the sign-up with that email.');
  });

  test('202 goes to verify with the email and resets the form', async () => {
    stubFetch(accepted());
    const { fillAll, accept, submit, labels } = renderRegister();
    fillAll();
    accept();

    submit();

    await waitFor(() => expect(router.push).toHaveBeenCalledExactlyOnceWith('/verify'));
    expect(screen.getByTestId('auth-flow')).toHaveTextContent(JSON.stringify({ email: 'ana@example.do' }));
    expect(screen.getByLabelText(labels.email)).toHaveValue('');
    expect(screen.getByLabelText(labels.password)).toHaveValue('');
    expect(screen.getByRole('checkbox')).not.toBeChecked();
  });

  test('the password never reaches the store or the url', async () => {
    stubFetch(accepted());
    const { fillAll, accept, submit } = renderRegister();
    fillAll();
    accept();

    submit();

    await waitFor(() => expect(router.push).toHaveBeenCalled());
    expect(screen.getByTestId('auth-flow').textContent).not.toContain(PASSWORD);
    expect(JSON.stringify(router.push.mock.calls)).not.toContain(PASSWORD);
  });

  test('double submit sends one request', async () => {
    let release: (response: Response) => void = () => undefined;
    const fetchMock = stubFetch(() => new Promise<Response>((resolve) => (release = resolve)));
    const { fillAll, accept } = renderRegister();
    fillAll();
    accept();
    const button = screen.getByRole('button', { name: en.auth.register.submit });
    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.submit(button.closest('form')!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    release(accepted());

    await waitFor(() => expect(router.push).toHaveBeenCalledOnce());
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  test('can submit again after a failure', async () => {
    const fetchMock = stubFetch(jsonResponse(500, errorBody('internal.unexpected')), accepted());
    const { fillAll, accept, submit } = renderRegister();
    fillAll();
    accept();

    submit();
    await screen.findByRole('alert');
    submit();

    await waitFor(() => expect(router.push).toHaveBeenCalledOnce());
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
