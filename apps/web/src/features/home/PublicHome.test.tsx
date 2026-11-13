import { screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { PublicHome } from './PublicHome';

describe('PublicHome', () => {
  test.each([
    ['es-DO', 'Iniciar sesión', 'Crear cuenta'],
    ['en', 'Log in', 'Sign up'],
  ] as const)('signed out shows Log in and Sign up, in %s', (locale, logIn, signUp) => {
    renderWithProviders(<PublicHome signedIn={false} />, { locale });

    expect(screen.getByRole('link', { name: logIn })).toHaveAttribute('href', `/${locale}/login`);
    expect(screen.getByRole('link', { name: signUp })).toHaveAttribute('href', `/${locale}/register`);
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
  });

  test.each([
    ['es-DO', 'No pudimos confirmar que tus otras sesiones se cerraron. Si te preocupa, cambia tu contraseña.'],
    ['en', 'We could not confirm that your other sessions were closed. If you are worried, change your password.'],
  ] as const)('an unconfirmed logout on all devices shows a notice, in %s', (locale, text) => {
    renderWithProviders(<PublicHome signedIn={false} notice="logout_all_failed" />, { locale });

    expect(screen.getByRole('status')).toHaveTextContent(text);
  });

  test('no notice, no banner', () => {
    renderWithProviders(<PublicHome signedIn={false} />, { locale: 'en' });

    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('signed in shows a Vehicles link instead', () => {
    renderWithProviders(<PublicHome signedIn />, { locale: 'en' });

    expect(screen.getByRole('link', { name: 'Vehicles' })).toHaveAttribute('href', '/en/vehicles');
    expect(screen.queryByRole('link', { name: 'Log in' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Sign up' })).not.toBeInTheDocument();
  });
});
