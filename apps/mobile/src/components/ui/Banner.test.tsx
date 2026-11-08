import { render, screen } from '@testing-library/react-native';

import { Banner } from './Banner';

test('error tone is an alert', async () => {
  await render(<Banner tone="error">Something went wrong.</Banner>);
  const banner = screen.getByRole('alert');
  expect(banner.props.accessibilityLiveRegion).toBe('polite');
  expect(screen.getByText('Something went wrong.')).toBeTruthy();
});

test('info tone is not an alert but is still announced politely', async () => {
  await render(<Banner tone="info">Saved.</Banner>);
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByText('Saved.')).toBeTruthy();
  expect(screen.getByTestId('banner').props.accessibilityLiveRegion).toBe('polite');
});
