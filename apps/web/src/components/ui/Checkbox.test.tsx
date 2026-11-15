import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { Checkbox } from './Checkbox';

test('the checkbox focus outline is navy, like the other controls', () => {
  render(<Checkbox label="Accept" />);

  const classes = screen.getByRole('checkbox', { name: 'Accept' }).className;
  expect(classes).toContain('focus-visible:outline-brand-navy');
  expect(classes).not.toContain('outline-brand-primary');
});
