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
    primary: 'bg-brand-navy',
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

  test('primary is navy with a navy focus ring', () => {
    render(<Button variant="primary">A</Button>);

    const classes = screen.getByRole('button', { name: 'A' }).className;
    expect(classes).toContain('bg-brand-navy');
    expect(classes).toContain('var(--color-brand-navy)');
    expect(classes).not.toContain('bg-brand-action');
  });

  test('primaryOnDark is a white fill with navy text and the surface ring (CC-11)', () => {
    render(<Button variant="primaryOnDark">B</Button>);

    const classes = screen.getByRole('button', { name: 'B' }).className.split(/\s+/);
    expect(classes).toContain('bg-surface');
    expect(classes).toContain('text-brand-navy');
    expect(classes).toContain('focus-visible:shadow-[0_0_0_3px_var(--color-surface)]');
    // The default ring's outer 3 px would be navy on navy.
    expect(classes.join(' ')).not.toContain('color-brand-navy)');
  });

  test('defaults to the primary variant and to type="button"', () => {
    render(<Button>Go</Button>);

    const button = screen.getByRole('button', { name: 'Go' });
    expect(button.className).toContain('bg-brand-navy');
    expect(button).toHaveAttribute('type', 'button');
  });
});
