import { screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, test } from 'vitest';
import { buttonClass } from '@/components/ui/Button';
import { LotErrorPanel } from '@/features/lot/components/LotErrorPanel';
import { renderWithProviders } from '@/testing/render';
import { PublicHome } from './PublicHome';

// The lot slot receives the actions node and decides where they sit (design §4.1).
const noLot = (actions: ReactNode) => (
  <>
    <p>lot slot</p>
    {actions}
  </>
);

describe('PublicHome', () => {
  test.each([
    ['es-DO', 'Iniciar sesión', 'Crear cuenta'],
    ['en', 'Log in', 'Sign up'],
  ] as const)('signed out shows Log in and Sign up, in %s', (locale, logIn, signUp) => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale });

    expect(screen.getByRole('link', { name: logIn })).toHaveAttribute('href', `/${locale}/login`);
    expect(screen.getByRole('link', { name: signUp })).toHaveAttribute('href', `/${locale}/register`);
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
  });

  test.each([
    ['es-DO', 'Consulta la disponibilidad del parqueo en tiempo real', 'Mira cuántos espacios hay libres antes de llegar.'],
    ['en', 'Check parking availability in real time', 'See how many spaces are free before you arrive.'],
  ] as const)('the hero has the title and subtitle, in %s', (locale, title, subtitle) => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale });

    expect(screen.getByRole('heading', { level: 1, name: title })).toBeInTheDocument();
    expect(screen.getByText(subtitle)).toBeInTheDocument();
  });

  test('the API down still renders the hero, Log in and Sign up', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={(actions) => (
        <>
          <LotErrorPanel />
          {actions}
        </>
      )} />, { locale: 'es-DO' });

    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Iniciar sesión' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Crear cuenta' })).toBeInTheDocument();
    expect(screen.getByText('La disponibilidad no está cargando ahora')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Reintentar' })).toBeInTheDocument();
  });

  test('the lot slot renders after the hero', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale: 'en' });

    expect(screen.getByText('lot slot')).toBeInTheDocument();
  });

  test.each([
    ['es-DO', 'No pudimos confirmar que tus otras sesiones se cerraron. Si te preocupa, cambia tu contraseña.'],
    ['en', 'We could not confirm that your other sessions were closed. If you are worried, change your password.'],
  ] as const)('an unconfirmed logout on all devices shows a notice, in %s', (locale, text) => {
    renderWithProviders(<PublicHome signedIn={false} notice="logout_all_failed" lot={noLot} />, { locale });

    expect(screen.getByRole('status')).toHaveTextContent(text);
  });

  test('the notice sits above the hero, outside the grid, so it is at the top at every width', () => {
    renderWithProviders(<PublicHome signedIn={false} notice="logout_all_failed" lot={noLot} />, { locale: 'en' });

    const banner = screen.getByRole('status');
    const hero = screen.getByRole('heading', { level: 1 });
    expect(banner.compareDocumentPosition(hero) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(banner.closest('[class*="lg:grid-cols"]')).toBeNull();
  });

  test('no notice, no banner', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale: 'en' });

    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('signed in shows the Vehicles link instead', () => {
    renderWithProviders(<PublicHome signedIn lot={noLot} />, { locale: 'en' });

    expect(screen.getByRole('link', { name: 'Vehicles' })).toHaveAttribute('href', '/en/vehicles');
    expect(screen.queryByRole('link', { name: 'Log in' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Sign up' })).not.toBeInTheDocument();
  });

  test('DOM order is hero, then the lot counter, then the actions (design §4.1)', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale: 'en' });

    const hero = screen.getByRole('heading', { level: 1 });
    const lot = screen.getByText('lot slot');
    const logIn = screen.getByRole('link', { name: 'Log in' });
    expect(hero.compareDocumentPosition(lot) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(lot.compareDocumentPosition(logIn) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  test('the hero is the wide column, row 1; the actions are handed to the slot to be placed under it', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale: 'en' });

    expect(screen.getByRole('heading', { level: 1 }).closest('[class*="lg:col-start-1"]')!.className).toContain('lg:row-start-1');
    const actions = screen.getByRole('link', { name: 'Log in' }).closest('[data-slot="actions"]')!;
    expect(actions.className).toContain('lg:col-start-1 lg:row-start-2');
  });

  test('Sign up is the outline button of the mockup', () => {
    renderWithProviders(<PublicHome signedIn={false} lot={noLot} />, { locale: 'en' });

    expect(screen.getByRole('link', { name: 'Sign up' })).toHaveAttribute('class', buttonClass('outline'));
    expect(screen.getByRole('link', { name: 'Log in' })).toHaveAttribute('class', buttonClass('primary'));
  });
});
