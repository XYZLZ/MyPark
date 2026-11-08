import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { Plate } from './Plate';

test('the plate is real text a screen reader can read', () => {
  render(<Plate value="A123456" />);

  expect(screen.getByText('A123456')).toBeVisible();
});

test('the plate text uses the plate typeface', () => {
  render(<Plate value="A123456" />);

  expect(screen.getByText('A123456').className.split(/\s+/)).toContain('font-plate');
});

test('a 10-character plate stays whole text in the fixed-width box', () => {
  render(<Plate value="ABCDE12345" />);

  const text = screen.getByText('ABCDE12345');
  expect(text).toHaveTextContent('ABCDE12345');
  expect(text.parentElement?.className).toContain('w-[142px]');
  // The condensed face is what lets 10 characters fit 142 px; no wrapping utilities that would hide overflow.
  expect(text.className).toContain('font-plate');
  expect(text.className).not.toContain('truncate');
});
