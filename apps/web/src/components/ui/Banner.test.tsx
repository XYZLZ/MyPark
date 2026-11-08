import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { Banner } from './Banner';

describe('Banner', () => {
  test('error tone is an alert, status tone is a status', () => {
    const { rerender } = render(<Banner tone="error">Failed</Banner>);
    expect(screen.getByRole('alert')).toHaveTextContent('Failed');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    rerender(<Banner tone="status">Saved</Banner>);
    expect(screen.getByRole('status')).toHaveTextContent('Saved');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  test('can receive focus from a script through its ref', () => {
    const ref = createRef<HTMLDivElement>();
    render(
      <Banner tone="error" ref={ref}>
        Failed
      </Banner>,
    );

    ref.current?.focus();
    expect(screen.getByRole('alert')).toHaveFocus();
  });
});
