import { render, screen } from '@testing-library/react-native';

import { TabIcon } from './TabIcon';

test.each(['home', 'vehicles', 'profile'] as const)('the %s icon is silent to screen readers', async (name) => {
  await render(<TabIcon name={name} focused={false} />);
  const hidden = screen.toJSON();
  expect(hidden).toMatchObject({ props: { accessibilityElementsHidden: true, importantForAccessibility: 'no-hide-descendants' } });
});
