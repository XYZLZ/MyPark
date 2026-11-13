import { fireEvent, screen } from '@testing-library/react-native';

import { useAuthStore } from '@/store/auth';
import { renderScreen } from '@/testing/render';

import { StorageNotice } from './StorageNotice';

jest.mock('react-native-safe-area-context', () => ({ useSafeAreaInsets: () => ({ top: 24, bottom: 0, left: 0, right: 0 }) }));

beforeEach(() => useAuthStore.setState(useAuthStore.getInitialState(), true));

const MESSAGE = 'We could not save your session on this device. You will need to log in the next time you open the app.';

test('renders nothing without a storage notice', async () => {
  await renderScreen(<StorageNotice />);
  expect(screen.queryByTestId('banner')).toBeNull();
  // No wrapper, no inset padding: the stack headers below already clear the status bar.
  expect(screen.toJSON()).toBeNull();
});

test('shows the storage_failed notice and clears it on dismiss', async () => {
  useAuthStore.getState().setNotice('storage_failed');
  await renderScreen(<StorageNotice />);

  expect(screen.getByText(MESSAGE)).toBeTruthy();
  // The notice sits at the very top, so it clears the status bar itself (inset 24 plus the 12 spacing).
  expect(screen.getByTestId('storage-notice').props.style).toMatchObject({ paddingTop: 36 });
  await fireEvent.press(screen.getByRole('link', { name: 'Dismiss' }));

  expect(screen.queryByText(MESSAGE)).toBeNull();
  expect(useAuthStore.getState().notice).toBeNull();
});

test('ignores other notices', async () => {
  useAuthStore.getState().setNotice('session_ended');
  await renderScreen(<StorageNotice />);
  expect(screen.queryByTestId('banner')).toBeNull();
});
