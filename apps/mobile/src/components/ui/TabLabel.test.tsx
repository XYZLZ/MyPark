import { screen } from '@testing-library/react-native';

import { renderScreen } from '@/testing/render';

import { TabLabel } from './TabLabel';

test('the focused tab is bold with a navy underline', async () => {
  await renderScreen(<TabLabel label="Vehicles" focused />);
  expect(screen.getByText('Vehicles').props.className).toContain('text-brand-navy');
  expect(screen.getByTestId('tab-underline').props.className).toContain('bg-brand-navy');
});

test('an unfocused tab is muted with no underline', async () => {
  await renderScreen(<TabLabel label="Vehicles" focused={false} />);
  expect(screen.getByText('Vehicles').props.className).toContain('text-ink-muted');
  expect(screen.getByTestId('tab-underline').props.className).toContain('bg-transparent');
});
