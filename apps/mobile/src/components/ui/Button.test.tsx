import { fireEvent, render, screen } from '@testing-library/react-native';
import { Animated, AccessibilityInfo } from 'react-native';

import { tokens } from '@/design/tokens';

import { Button } from './Button';

let reduceMotion = false;

beforeEach(() => {
  reduceMotion = false;
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockImplementation(async () => reduceMotion);
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockImplementation(() => ({ remove: jest.fn() }));
});

afterEach(() => jest.restoreAllMocks());

test('loading keeps it pressable and announces busy', async () => {
  const onPress = jest.fn();
  await render(<Button variant="primary" label="Save" loading onPress={onPress} />);
  const button = screen.getByRole('button', { name: 'Save' });
  expect(button.props.accessibilityState).toMatchObject({ busy: true });
  expect(button.props.accessibilityState.disabled).toBeFalsy();
  // Focus stays on the control, but a second tap while in flight must not submit twice.
  await fireEvent.press(button);
  expect(onPress).not.toHaveBeenCalled();
});

test('presses call onPress when idle', async () => {
  const onPress = jest.fn();
  await render(<Button variant="primary" label="Save" onPress={onPress} />);
  await fireEvent.press(screen.getByRole('button', { name: 'Save' }));
  expect(onPress).toHaveBeenCalledTimes(1);
});

test('disabled is reported to assistive tech', async () => {
  const onPress = jest.fn();
  await render(<Button variant="primary" label="Save" disabled onPress={onPress} />);
  const button = screen.getByRole('button', { name: 'Save' });
  expect(button.props.accessibilityState).toMatchObject({ disabled: true });
  await fireEvent.press(button);
  expect(onPress).not.toHaveBeenCalled();
});

test('has a 48 dp minimum height', async () => {
  await render(<Button variant="quiet" label="Save" onPress={jest.fn()} />);
  expect(screen.getByRole('button', { name: 'Save' }).props.className).toContain('min-h-12');
});

test('animates the press with a 180 ms scale', async () => {
  const timing = jest.spyOn(Animated, 'timing');
  await render(<Button variant="primary" label="Save" onPress={jest.fn()} />);
  await fireEvent(screen.getByRole('button', { name: 'Save' }), 'pressIn');
  expect(timing).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ toValue: 0.98, duration: 180 }));
});

test('skips the press animation when reduce motion is on', async () => {
  reduceMotion = true;
  const timing = jest.spyOn(Animated, 'timing');
  await render(<Button variant="primary" label="Save" onPress={jest.fn()} />);
  await fireEvent(screen.getByRole('button', { name: 'Save' }), 'pressIn');
  expect(timing).not.toHaveBeenCalled();
});

// Spec §9.5 "Same": each variant matches the web Button (apps/web/src/components/ui/Button.tsx).
const classesOf = (element: { props: { className?: string } }) => (element.props.className ?? '').split(/\s+/);

test('onDark is a transparent outline with white text, like web', async () => {
  await render(<Button variant="onDark" label="Log in" onPress={jest.fn()} />);
  const surface = classesOf(screen.getByRole('button', { name: 'Log in' }));
  expect(surface).toEqual(expect.arrayContaining(['bg-transparent', 'border-2', 'border-brand-primary-contrast']));
  expect(surface).not.toContain('bg-surface');
  // The focus border must stay visible on navy, so it is not the navy brand color.
  expect(surface).not.toContain('focus:border-brand-primary');
  expect(classesOf(screen.getByText('Log in'))).toContain('text-brand-primary-contrast');
});

test('quiet has no border, like web', async () => {
  await render(<Button variant="quiet" label="Back" onPress={jest.fn()} />);
  const surface = classesOf(screen.getByRole('button', { name: 'Back' }));
  expect(surface).toContain('bg-transparent');
  expect(surface).not.toContain('border-2');
  expect(surface).not.toContain('border-control-line');
  expect(classesOf(screen.getByText('Back'))).toContain('text-brand-primary');
});

test('disabled has no border except on onDark, like web', async () => {
  await render(<Button variant="quiet" label="Back" disabled onPress={jest.fn()} />);
  expect(classesOf(screen.getByRole('button', { name: 'Back' }))).not.toContain('border-2');
  await render(<Button variant="onDark" label="Log in" disabled onPress={jest.fn()} />);
  expect(classesOf(screen.getByRole('button', { name: 'Log in' }))).toEqual(
    expect.arrayContaining(['bg-disabled-bg', 'border-2', 'border-disabled-ink']),
  );
});

test.each([
  ['primary', tokens.brandActionContrast],
  ['onDark', tokens.brandPrimaryContrast],
  ['quiet', tokens.brandPrimary],
  ['danger', tokens.brandPrimaryContrast],
] as const)('the %s spinner uses the text color, like web', async (variant, color) => {
  await render(<Button variant={variant} label="Save" loading onPress={jest.fn()} />);
  expect(screen.getByTestId('button-spinner').props.color).toBe(color);
});
