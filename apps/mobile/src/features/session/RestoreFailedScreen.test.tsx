import { fireEvent, screen, waitFor } from '@testing-library/react-native';

import { session } from '@/services/runtime';
import { renderScreen } from '@/testing/render';

import { RestoreFailedScreen } from './RestoreFailedScreen';

jest.mock('@/services/runtime', () => ({
  session: { restore: jest.fn(), logout: jest.fn() },
}));

const restore = jest.mocked(session.restore);
const logout = jest.mocked(session.logout);

beforeEach(() => {
  restore.mockReset().mockResolvedValue(undefined);
  logout.mockReset().mockResolvedValue(undefined);
});

test('offers Retry and Log out and keeps the token until Log out', async () => {
  await renderScreen(<RestoreFailedScreen />);
  expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Log out' })).toBeTruthy();

  await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
  expect(logout).not.toHaveBeenCalled();

  await fireEvent.press(screen.getByRole('button', { name: 'Log out' }));
  await waitFor(() => expect(logout).toHaveBeenCalledTimes(1));
});

test('a double tap on Retry runs one restore', async () => {
  let finish: () => void = () => undefined;
  restore.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));
  await renderScreen(<RestoreFailedScreen />);
  const retry = screen.getByRole('button', { name: 'Try again' });
  await fireEvent.press(retry);
  await fireEvent.press(retry);
  expect(restore).toHaveBeenCalledTimes(1);
  finish();
});
