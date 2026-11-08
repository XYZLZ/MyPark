import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import ErrorPage from './error';

describe('error.tsx', () => {
  test('offers Retry and shows a translated message, never error.message', () => {
    const reset = vi.fn();
    renderWithProviders(<ErrorPage error={new Error('pq: password authentication failed')} reset={reset} />, { locale: 'en' });

    expect(screen.getByRole('heading', { name: en.errorPage.title })).toBeInTheDocument();
    expect(screen.getByText(en.errors.fallback)).toBeInTheDocument();
    expect(screen.queryByText(/pq:/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: en.errorPage.retry }));
    expect(reset).toHaveBeenCalledTimes(1);
    expect(router.refresh).toHaveBeenCalledTimes(1);
  });
});
