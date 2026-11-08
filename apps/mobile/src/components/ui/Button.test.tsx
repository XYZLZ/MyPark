import { fireEvent, render, screen } from '@testing-library/react-native';
import { Animated, AccessibilityInfo } from 'react-native';

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
