import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { Field } from './Field';

function Sample({ invalid, message }: { invalid: boolean; message?: string }) {
  return (
    <Field.Root name="plate" invalid={invalid}>
      <Field.Label>Plate</Field.Label>
      <Field.Control>
        <input />
      </Field.Control>
      <Field.Hint>Letters and numbers</Field.Hint>
      <Field.Error message={message} />
    </Field.Root>
  );
}

describe('Field', () => {
  test('label is associated with the control', () => {
    render(<Sample invalid={false} />);

    const input = screen.getByLabelText('Plate');
    expect(input.tagName).toBe('INPUT');
    expect(input).toHaveAttribute('name', 'plate');
  });

  test('invalid control gets aria-invalid and aria-describedby pointing at the error', () => {
    render(<Sample invalid message="Already registered" />);

    const input = screen.getByLabelText('Plate');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveAccessibleDescription(/Already registered/);
    expect(input).toHaveAccessibleDescription(/Letters and numbers/);
  });

  test('valid control has no aria-invalid and does not show the error', () => {
    render(<Sample invalid={false} message="Already registered" />);

    const input = screen.getByLabelText('Plate');
    expect(input).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByText('Already registered')).not.toBeInTheDocument();
    expect(input).toHaveAccessibleDescription('Letters and numbers');
  });

  test('keeps a describedby the child already had', () => {
    render(
      <Field.Root name="x" invalid={false}>
        <Field.Control>
          <input aria-describedby="extra" />
        </Field.Control>
      </Field.Root>,
    );

    expect(document.querySelector('input')?.getAttribute('aria-describedby')).toMatch(/^extra /);
  });
});
