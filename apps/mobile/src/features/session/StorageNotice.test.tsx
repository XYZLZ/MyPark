import { fireEvent, screen } from '@testing-library/react-native';

import { useAuthStore } from '@/store/auth';
import { renderScreen } from '@/testing/render';

import { StorageNotice } from './StorageNotice';

beforeEach(() => useAuthStore.setState(useAuthStore.getInitialState(), true));

const MESSAGE = 'We could not save your session on this device. You will need to log in the next time you open the app.';

test('renders nothing without a storage notice', async () => {
  await renderScreen(<StorageNotice />);
  expect(screen.queryByTestId('banner')).toBeNull();
});

test('shows the storage_failed notice and clears it on dismiss', async () => {
  useAuthStore.getState().setNotice('storage_failed');
  await renderScreen(<StorageNotice />);

  expect(screen.getByText(MESSAGE)).toBeTruthy();
  await fireEvent.press(screen.getByRole('link', { name: 'Dismiss' }));

  expect(screen.queryByText(MESSAGE)).toBeNull();
  expect(useAuthStore.getState().notice).toBeNull();
});

test('ignores other notices', async () => {
  useAuthStore.getState().setNotice('session_ended');
  await renderScreen(<StorageNotice />);
  expect(screen.queryByTestId('banner')).toBeNull();
});
