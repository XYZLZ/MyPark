import { fireEvent, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { LegalUnavailable } from './LegalUnavailable';

test('legal fetch failure shows a retry panel', () => {
  renderWithProviders(<LegalUnavailable />, { locale: 'en' });

  expect(screen.getByRole('heading', { name: 'Sign-up is unavailable' })).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('We could not load the legal texts');

  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

  expect(router.refresh).toHaveBeenCalledOnce();
});
