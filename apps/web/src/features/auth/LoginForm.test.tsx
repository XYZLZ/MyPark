import { adminFixture, errorBody, successBody, userFixture } from '@mypark/contracts/fixtures';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { hardNavigate } from '@/lib/browser/navigate';
import { AuthFlowProbe, SeedAuthFlow } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { LoginForm } from './LoginForm';

vi.mock('@/lib/browser/navigate', () => ({ hardNavigate: vi.fn() }));

const L = en.auth.fields;
const PASSWORD = 'Sup3rSecret!!';

function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

function submit() {
  fireEvent.click(screen.getByRole('button', { name: 'Log in' }));
}

function renderLogin(props: Partial<Parameters<typeof LoginForm>[0]> = {}) {
  return renderWithProviders(
    <>
      <LoginForm heading="driver" {...props} />
      <AuthFlowProbe />
    </>,
    { locale: 'en' },
  );
}

function fillCredentials() {
  fill(L.email, 'ana@example.do');
  fill(L.password, PASSWORD);
}

const loginOk = (user = userFixture) => jsonResponse(200, successBody({ user, legal_acceptance_required: false }));

describe('LoginForm', () => {
  test('validates before calling the API', async () => {
    const fetchMock = stubFetch(loginOk());
    renderLogin();

    submit();

    expect(await screen.findAllByText('This field is required.')).toHaveLength(2);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByLabelText(L.email)).toHaveFocus();
    expect(screen.getByLabelText(L.email)).toBeInvalid();
  });

  test('uses the right autocomplete values', () => {
    renderLogin();

    expect(screen.getByLabelText(L.email)).toHaveAttribute('autocomplete', 'email');
    expect(screen.getByLabelText(L.password)).toHaveAttribute('autocomplete', 'current-password');
  });

  test('success redirects by role to the user locale', async () => {
    const fetchMock = stubFetch(loginOk({ ...userFixture, locale: 'en' }));
    renderLogin();
    fillCredentials();

    submit();

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/en/vehicles'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login');
    expect(sentBody(fetchMock)).toEqual({ email: 'ana@example.do', password: PASSWORD });
  });

  test('an admin goes to the admin landing page', async () => {
    stubFetch(loginOk(adminFixture));
    renderLogin({ heading: 'admin' });
    fillCredentials();

    submit();

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/es-DO/admin'));
    expect(screen.getByRole('heading', { name: 'Admin log in' })).toBeInTheDocument();
  });

  test('success honors a safe next', async () => {
    stubFetch(loginOk());
    renderLogin({ next: '/en/vehicles/new?x=1' });
    fillCredentials();

    submit();

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/en/vehicles/new?x=1'));
  });

  test.each(['//evil.example', '/\\evil.example', '/%5Cevil.example', 'https://evil.example'])(
    'an unsafe next (%s) falls back to the role home',
    async (next) => {
      stubFetch(loginOk());
      renderLogin({ next });
      fillCredentials();

      submit();

      await waitFor(() => expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/es-DO/vehicles'));
    },
  );

  test('wrong credentials show one banner', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.invalid_credentials')));
    renderLogin();
    fillCredentials();

    submit();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('The email or password is not correct.');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    await waitFor(() => expect(alert).toHaveFocus());
    expect(hardNavigate).not.toHaveBeenCalled();
  });

  test('email_not_verified goes to verify keeping only the email', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.email_not_verified')));
    renderLogin();
    fillCredentials();

    submit();

    await waitFor(() => expect(router.push).toHaveBeenCalledExactlyOnceWith('/verify'));
    expect(screen.getByTestId('auth-flow')).toHaveTextContent(JSON.stringify({ email: 'ana@example.do' }));
  });

  test('account_suspended shows a banner', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.account_suspended')));
    renderLogin();
    fillCredentials();

    submit();

    expect(await screen.findByRole('alert')).toHaveTextContent('Your account is suspended. Contact support.');
  });

  test('429 shows the wait time and disables submit', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 42 })));
    renderLogin();
    fillCredentials();

    submit();

    expect(await screen.findByRole('alert')).toHaveTextContent('42');
    expect(screen.getByRole('button', { name: 'Log in' })).toBeDisabled();
  });

  test('429 with no usable wait time shows 60 s and locks submit', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded'), { 'Retry-After': 'soon' }));
    renderLogin();
    fillCredentials();

    submit();

    expect(await screen.findByRole('alert')).toHaveTextContent('Try again in 60 s.');
    expect(screen.getByRole('button', { name: 'Log in' })).toBeDisabled();
  });

  test('a network failure shows a retry message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    renderLogin();
    fillCredentials();

    submit();

    expect(await screen.findByRole('alert')).toHaveTextContent('We could not connect.');
  });

  test('session_expired reason shows a status banner', () => {
    renderLogin({ reason: 'session_expired' });

    expect(screen.getByRole('status')).toHaveTextContent('Your session ended. Log in again.');
  });

  test('no banner for any other reason', () => {
    renderLogin({ reason: 'whatever' });

    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('pre-fills the email from the auth flow and focuses the password', async () => {
    renderWithProviders(
      <SeedAuthFlow email="ana@example.do">
        <LoginForm heading="driver" />
      </SeedAuthFlow>,
      { locale: 'en' },
    );

    expect(await screen.findByLabelText(L.email)).toHaveValue('ana@example.do');
    expect(screen.getByLabelText(L.password)).toHaveValue('');
    await waitFor(() => expect(screen.getByLabelText(L.password)).toHaveFocus());
  });

  test('password is not carried on any exit', async () => {
    stubFetch(jsonResponse(403, errorBody('auth.email_not_verified')), loginOk());
    renderLogin();
    fillCredentials();

    submit();
    await waitFor(() => expect(router.push).toHaveBeenCalled());
    expect(JSON.stringify(router.push.mock.calls)).not.toContain(PASSWORD);
    expect(screen.getByTestId('auth-flow').textContent).not.toContain(PASSWORD);

    fillCredentials();
    submit();
    await waitFor(() => expect(hardNavigate).toHaveBeenCalled());
    expect(JSON.stringify(vi.mocked(hardNavigate).mock.calls)).not.toContain(PASSWORD);
    expect(screen.getByTestId('auth-flow').textContent).not.toContain(PASSWORD);
    expect(screen.getByLabelText(L.password)).toHaveValue('');
  });

  test('the wait message clears and submit re-enables when the wait ends', async () => {
    vi.useFakeTimers();
    try {
      stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 3 })));
      renderLogin();
      fillCredentials();

      submit();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.getByRole('alert')).toHaveTextContent('Try again in 3 s.');
      expect(screen.getByRole('button', { name: 'Log in' })).toBeDisabled();

      // Each tick is scheduled after the previous render, so advance one second at a time.
      for (let second = 0; second < 3; second++) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(1000);
        });
      }

      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Log in' })).toBeEnabled();
    } finally {
      vi.useRealTimers();
    }
  });
});
