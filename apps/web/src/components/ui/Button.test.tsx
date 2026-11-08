import { fireEvent, render, screen } from '@testing-library/react';
import type { SubmitEvent } from 'react';
import { describe, expect, test, vi } from 'vitest';
import { Button } from './Button';

function submitSpy() {
  return vi.fn((event: SubmitEvent) => event.preventDefault());
}

describe('Button', () => {
  test('loading sets aria-busy, shows the verb and stays focusable', () => {
    render(
      <Button loading loadingLabel="Saving">
        Save
      </Button>,
    );

    const button = screen.getByRole('button', { name: 'Saving' });
    expect(button).toHaveAttribute('aria-busy', 'true');
    expect(button).not.toBeDisabled();
    expect(screen.queryByText('Save')).not.toBeInTheDocument();
    button.focus();
    expect(button).toHaveFocus();
  });

  test('loading swallows clicks, so a double tap cannot submit twice', () => {
    const onClick = vi.fn();
    const onSubmit = submitSpy();
    render(
      <form onSubmit={onSubmit}>
        <Button type="submit" loading loadingLabel="Saving" onClick={onClick}>
          Save
        </Button>
      </form>,
    );

    fireEvent.click(screen.getByRole('button'));

    expect(onClick).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  test('is not busy and submits when idle', () => {
    const onSubmit = submitSpy();
    render(
      <form onSubmit={onSubmit}>
        <Button type="submit">Save</Button>
      </form>,
    );

    const button = screen.getByRole('button', { name: 'Save' });
    expect(button).not.toHaveAttribute('aria-busy');
    fireEvent.click(button);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  // Each variant is identified by the class that carries its look, and must not carry another's.
  const SIGNATURE = {
    primary: 'bg-brand-action',
    onDark: 'border-current',
    quiet: 'hover:bg-brand-primary-tint',
    danger: 'bg-danger',
  } as const;

  test.each(['primary', 'onDark', 'quiet', 'danger'] as const)('renders the %s variant', (variant) => {
    render(<Button variant={variant}>Go</Button>);

    const classes = screen.getByRole('button', { name: 'Go' }).className.split(/\s+/);
    expect(classes).toContain(SIGNATURE[variant]);
    for (const [other, signature] of Object.entries(SIGNATURE)) {
      if (other !== variant) expect(classes).not.toContain(signature);
    }
  });

  test('primaryOnDark keeps the primary fill but swaps the focus ring to a 3 px surface ring (CC-11)', () => {
    render(
      <>
        <Button variant="primary">A</Button>
        <Button variant="primaryOnDark">B</Button>
      </>,
    );

    const primary = screen.getByRole('button', { name: 'A' }).className.split(/\s+/);
    const onDark = screen.getByRole('button', { name: 'B' }).className.split(/\s+/);
    expect(onDark).toContain('bg-brand-action');
    expect(onDark).toContain('focus-visible:shadow-[0_0_0_3px_var(--color-surface)]');
    expect(onDark.join(' ')).not.toContain('color-brand-primary');
    // The default ring would leave only 2 px of white on navy, because its outer 3 px is navy.
    expect(primary.join(' ')).toContain('color-brand-primary');
  });

  test('defaults to the primary variant and to type="button"', () => {
    render(<Button>Go</Button>);

    const button = screen.getByRole('button', { name: 'Go' });
    expect(button.className).toContain('bg-brand-action');
    expect(button).toHaveAttribute('type', 'button');
  });
});
