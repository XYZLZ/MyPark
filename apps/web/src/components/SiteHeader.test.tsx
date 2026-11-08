import { screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { SiteHeader } from './SiteHeader';

describe('SiteHeader', () => {
  test('starts with a skip link to main content', () => {
    renderWithProviders(<SiteHeader appName="MyPark" languageSwitch={<span>switch</span>} />, { locale: 'en' });

    const skip = screen.getByRole('link', { name: 'Skip to main content' });
    expect(skip).toHaveAttribute('href', '#main');
    // First in the tab order: nothing focusable precedes it in the document.
    const focusables = screen.getAllByRole('link');
    expect(focusables[0]).toBe(skip);
  });

  test('shows the app name, the optional nav and the language switch', () => {
    renderWithProviders(
      <SiteHeader appName="MyPark" nav={<button type="button">Vehicles</button>} languageSwitch={<span>switch</span>} />,
    );

    expect(screen.getByRole('link', { name: /MyPark/ })).toHaveAttribute('href', '/es-DO');
    expect(screen.getByRole('button', { name: 'Vehicles' })).toBeInTheDocument();
    expect(screen.getByText('switch')).toBeInTheDocument();
  });

  test('omits the nav when none is given', () => {
    renderWithProviders(<SiteHeader appName="MyPark" languageSwitch={<span>switch</span>} />);

    expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
  });
});
