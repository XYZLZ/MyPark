import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { LanguageSwitch } from './LanguageSwitch';

describe('LanguageSwitch', () => {
  test('each link carries lang and hrefLang', () => {
    renderWithProviders(<LanguageSwitch />);

    const en = screen.getByRole('link', { name: 'English' });
    expect(en).toHaveAttribute('lang', 'en');
    expect(en).toHaveAttribute('hreflang', 'en');
    expect(en).toHaveAttribute('href', '/en');
    const es = screen.getByRole('link', { name: 'Español' });
    expect(es).toHaveAttribute('lang', 'es-DO');
    expect(es).toHaveAttribute('hreflang', 'es-DO');
  });

  test('marks the current language without relying on color', () => {
    renderWithProviders(<LanguageSwitch />, { locale: 'en' });

    expect(screen.getByRole('link', { name: 'English' })).toHaveAttribute('aria-current', 'true');
    expect(screen.getByRole('link', { name: 'Español' })).not.toHaveAttribute('aria-current');
  });

  test('reports the chosen language through onSwitch', () => {
    const onSwitch = vi.fn();
    renderWithProviders(<LanguageSwitch onSwitch={onSwitch} />);

    fireEvent.click(screen.getByRole('link', { name: 'English' }));

    expect(onSwitch).toHaveBeenCalledExactlyOnceWith('en');
  });
});
