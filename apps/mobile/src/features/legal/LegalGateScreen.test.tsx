import { errorBody, legalCurrentFixture, successBody, userFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { http, session } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { LegalGateScreen } from './LegalGateScreen';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));
jest.mock('@/services/runtime', () => ({
  http: { request: jest.fn() },
  session: { logout: jest.fn(), signOutLocal: jest.fn().mockResolvedValue(undefined) },
}));

const request = jest.mocked(http.request);
const logout = jest.mocked(session.logout);
const signOutLocal = jest.mocked(session.signOutLocal);
const NEXT = { ...legalCurrentFixture, terms: { ...legalCurrentFixture.terms, version: '2026-12-01' } };

beforeEach(() => {
  request.mockReset();
  logout.mockReset().mockResolvedValue('revoked');
  signOutLocal.mockClear();
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthStore.getState().setSession({ accessToken: 'a', user: userFixture, legalPending: true });
});

test('legal gate blocks', async () => {
  request.mockResolvedValue(reply(200, successBody(legalCurrentFixture)));
  await renderScreen(<LegalGateScreen />);

  expect(await screen.findByRole('button', { name: 'Accept and continue' })).toBeTruthy();
  expect(screen.getByText('We updated our legal texts')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Accept and continue' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Log out' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Delete my account' })).toBeTruthy();
  // Nothing from the signed-in app renders behind the gate, and no vehicles are requested.
  expect(screen.queryByText('My vehicles')).toBeNull();
  expect(request.mock.calls.every(([req]) => req.path === '/v1/legal/current')).toBe(true);
});

test('accept sends current versions and clears legalPending', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(legalCurrentFixture)));
  await renderScreen(<LegalGateScreen />);
  await screen.findByRole('button', { name: 'Accept and continue' });

  request.mockResolvedValueOnce(reply(204));
  await fireEvent.press(screen.getByRole('button', { name: 'Accept and continue' }));

  await waitFor(() => expect(useAuthStore.getState().legalPending).toBe(false));
  expect(request).toHaveBeenLastCalledWith({
    method: 'POST',
    path: '/v1/me/legal-acceptance',
    body: { terms_version: legalCurrentFixture.terms.version, privacy_version: legalCurrentFixture.privacy.version },
    auth: true,
  });
});

test('version_outdated shows a banner and re-reads', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(legalCurrentFixture)));
  await renderScreen(<LegalGateScreen />);
  await screen.findByRole('button', { name: 'Accept and continue' });

  request.mockResolvedValueOnce(reply(409, errorBody('legal.version_outdated')));
  request.mockResolvedValueOnce(reply(200, successBody(NEXT)));
  await fireEvent.press(screen.getByRole('button', { name: 'Accept and continue' }));

  expect(await screen.findByText('The legal texts changed. Read and accept them again.')).toBeTruthy();
  await waitFor(() => expect(request.mock.calls.filter(([req]) => req.path === '/v1/legal/current')).toHaveLength(2));
  expect(await screen.findByText('Version 2026-12-01')).toBeTruthy();
  expect(useAuthStore.getState().legalPending).toBe(true);

  // The next attempt sends the re-read version.
  request.mockResolvedValueOnce(reply(204));
  await fireEvent.press(screen.getByRole('button', { name: 'Accept and continue' }));
  await waitFor(() => expect(useAuthStore.getState().legalPending).toBe(false));
  expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ body: expect.objectContaining({ terms_version: '2026-12-01' }) }));
});

test('logout works behind the gate', async () => {
  request.mockResolvedValue(reply(200, successBody(legalCurrentFixture)));
  await renderScreen(<LegalGateScreen />);
  await screen.findByRole('button', { name: 'Accept and continue' });

  await fireEvent.press(screen.getByRole('button', { name: 'Log out' }));
  await waitFor(() => expect(logout).toHaveBeenCalledWith('this'));
});

test('logout and delete stay reachable when the legal texts cannot load', async () => {
  request.mockResolvedValue(reply(500, errorBody('internal.unexpected')));
  await renderScreen(<LegalGateScreen />);

  expect(await screen.findByRole('button', { name: 'Try again' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Accept and continue' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Log out' })).toBeTruthy();
  expect(screen.getByLabelText('Password')).toBeTruthy();
});

test('delete works behind the gate', async () => {
  request.mockResolvedValueOnce(reply(200, successBody(legalCurrentFixture)));
  await renderScreen(<LegalGateScreen />);
  await screen.findByRole('button', { name: 'Accept and continue' });

  request.mockResolvedValueOnce(reply(204));
  await fireEvent.changeText(screen.getByLabelText('Password'), 'hunter2-correct-horse');
  await fireEvent.press(screen.getByRole('button', { name: 'Delete my account' }));

  await waitFor(() => expect(signOutLocal).toHaveBeenCalled());
  expect(request).toHaveBeenLastCalledWith(
    expect.objectContaining({ method: 'DELETE', path: '/v1/me', body: { password: 'hunter2-correct-horse' }, timeoutMs: 20_000 }),
  );
});
