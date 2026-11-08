import { errorBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { AuthFlowProbe, SeedAuthFlow } from '@/testing/auth-flow';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { ResetPasswordForm } from './ResetPasswordForm';

const L = en.auth.fields;
const PASSWORD = 'An0therSecret!!';

function renderReset() {
  renderWithProviders(
    <>
      <SeedAuthFlow email="ana@example.do">
        <ResetPasswordForm />
      </SeedAuthFlow>
      <AuthFlowProbe />
    </>,
    { locale: 'en' },
  );
}

async function fillAndSubmit() {
  await screen.findByLabelText(L.email);
  fireEvent.change(screen.getByLabelText(L.code), { target: { value: '123456' } });
  fireEvent.change(screen.getByLabelText(L.newPassword), { target: { value: PASSWORD } });
  fireEvent.click(screen.getByRole('button', { name: en.auth.reset.submit }));
}

describe('ResetPasswordForm', () => {
  test('success logs out locally then goes to login pre-filled', async () => {
    const fetchMock = stubFetch(jsonResponse(204), jsonResponse(204));
    renderReset();

    await fillAndSubmit();

    await waitFor(() => expect(router.push).toHaveBeenCalledExactlyOnceWith('/login'));
    expect(fetchMock.mock.calls.map((call) => [call[0], call[1]!.method])).toEqual([
      ['/api/v1/auth/password/reset', 'POST'],
      ['/api/v1/auth/logout', 'POST'],
    ]);
    expect(sentBody(fetchMock, 0)).toEqual({ email: 'ana@example.do', code: '123456', new_password: PASSWORD });
    // The logout finished before the navigation.
    expect(fetchMock.mock.invocationCallOrder[1]).toBeLessThan(router.push.mock.invocationCallOrder[0]!);
    expect(screen.getByTestId('auth-flow')).toHaveTextContent(JSON.stringify({ email: 'ana@example.do' }));
    expect(screen.getByLabelText(L.newPassword)).toHaveValue('');
  });

  test('a failed local logout does not trap the user on the form', async () => {
    stubFetch(jsonResponse(204), jsonResponse(401, errorBody('auth.unauthenticated')));
    renderReset();

    await fillAndSubmit();

    await waitFor(() => expect(router.push).toHaveBeenCalledExactlyOnceWith('/login'));
  });

  test('uses new-password and one-time-code autocomplete', async () => {
    renderReset();

    expect(await screen.findByLabelText(L.newPassword)).toHaveAttribute('autocomplete', 'new-password');
    expect(screen.getByLabelText(L.code)).toHaveAttribute('autocomplete', 'one-time-code');
  });

  test('code_invalid maps to code', async () => {
    const fetchMock = stubFetch(jsonResponse(422, errorBody('auth.code_invalid')));
    renderReset();

    await fillAndSubmit();

    await waitFor(() => expect(screen.getByLabelText(L.code)).toBeInvalid());
    expect(screen.getByLabelText(L.code)).toHaveAccessibleDescription('The code is not valid or has expired.');
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(router.push).not.toHaveBeenCalled();
  });

  test('the new password never reaches the store or the url', async () => {
    stubFetch(jsonResponse(204), jsonResponse(204));
    renderReset();

    await fillAndSubmit();

    await waitFor(() => expect(router.push).toHaveBeenCalled());
    expect(screen.getByTestId('auth-flow').textContent).not.toContain(PASSWORD);
    expect(JSON.stringify(router.push.mock.calls)).not.toContain(PASSWORD);
  });
});
