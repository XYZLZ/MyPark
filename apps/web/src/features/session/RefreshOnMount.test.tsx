import { StrictMode } from 'react';
import { render } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { router } from '@/testing/navigation';
import { RefreshOnMount } from './RefreshOnMount';

describe('RefreshOnMount', () => {
  test('refreshes once on mount and not again on re-render or StrictMode', () => {
    const { rerender } = render(
      <StrictMode>
        <RefreshOnMount />
      </StrictMode>,
    );
    rerender(
      <StrictMode>
        <RefreshOnMount />
      </StrictMode>,
    );

    expect(router.refresh).toHaveBeenCalledTimes(1);
  });
});
