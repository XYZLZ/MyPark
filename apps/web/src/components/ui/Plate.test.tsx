import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { Plate } from './Plate';

test('the plate is real text a screen reader can read', () => {
  render(<Plate value="A123456" />);

  expect(screen.getByText('A123456')).toBeVisible();
});
