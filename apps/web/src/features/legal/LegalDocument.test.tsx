import { screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { LegalDocument, isLegalKind } from './LegalDocument';

test('renders the document with the demo notice', () => {
  renderWithProviders(<LegalDocument kind="privacy" />, { locale: 'en' });

  expect(screen.getByRole('heading', { name: 'Privacy policy' })).toBeInTheDocument();
  expect(screen.getByText('Sample text for an academic demo.')).toBeInTheDocument();
});

test('only terms and privacy are legal kinds', () => {
  expect(isLegalKind('terms')).toBe(true);
  expect(isLegalKind('privacy')).toBe(true);
  expect(isLegalKind('cookies')).toBe(false);
  expect(isLegalKind('constructor')).toBe(false);
});
