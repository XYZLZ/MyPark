import { render, screen } from '@testing-library/react-native';

import { TextField } from './TextField';

test('announces the error politely and includes it in the label', async () => {
  await render(<TextField label="Email" error="Enter a valid email address." hint="name@example.com" />);
  const error = screen.getByText('Enter a valid email address.');
  expect(error.props.accessibilityLiveRegion).toBe('polite');
  const input = screen.getByLabelText('Email, Enter a valid email address.');
  expect(input.props.accessibilityHint).toBe('name@example.com');
  expect(input.props.className).toContain('min-h-12');
});

test('without an error the label is just the label and no error is rendered', async () => {
  await render(<TextField label="Email" />);
  expect(screen.getByLabelText('Email')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
});

test('forwards the ref to the input', async () => {
  const ref = { current: null as null | { focus: () => void } };
  await render(<TextField ref={ref as never} label="Email" />);
  expect(typeof ref.current?.focus).toBe('function');
});
