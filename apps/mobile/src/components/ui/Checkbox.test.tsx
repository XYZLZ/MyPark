import { fireEvent, render, screen } from '@testing-library/react-native';

import { Checkbox } from './Checkbox';

test('reports role and checked state', async () => {
  const onChange = jest.fn();
  const { rerender } = await render(<Checkbox label="I accept" checked={false} onChange={onChange} />);
  const box = screen.getByRole('checkbox', { name: 'I accept' });
  expect(box.props.accessibilityState).toMatchObject({ checked: false });
  expect(box.props.className).toContain('min-h-12');

  await fireEvent.press(box);
  expect(onChange).toHaveBeenCalledWith(true);

  await rerender(<Checkbox label="I accept" checked onChange={onChange} />);
  expect(screen.getByRole('checkbox', { name: 'I accept' }).props.accessibilityState).toMatchObject({ checked: true });
  await fireEvent.press(screen.getByRole('checkbox', { name: 'I accept' }));
  expect(onChange).toHaveBeenLastCalledWith(false);
});

test('shows the error politely', async () => {
  await render(<Checkbox label="I accept" checked={false} onChange={jest.fn()} error="This field is required." />);
  expect(screen.getByText('This field is required.').props.accessibilityLiveRegion).toBe('polite');
});
