import { fireEvent, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { renderWithProviders } from '@/testing/render';
import { usePathname } from '@/testing/navigation';
import { AppShell, type ShellVariant } from './AppShell';

function renderShell(variant: ShellVariant, extra: { greetingName?: string; footer?: boolean } = {}) {
  return renderWithProviders(
    <AppShell
      variant={variant}
      greetingName={extra.greetingName}
      languageSwitch={<span>switch</span>}
      sidebarFooter={extra.footer === false ? null : <button type="button">Footer action</button>}
    >
      <p>page body</p>
    </AppShell>,
    { locale: 'en' },
  );
}

describe('AppShell', () => {
  beforeEach(() => usePathname.mockReturnValue('/'));

  test('starts with a skip link to main', () => {
    renderShell('guest');

    const skip = screen.getByRole('link', { name: en.common.skipToContent });
    expect(skip).toHaveAttribute('href', '#main');
    expect(screen.getAllByRole('link')[0]).toBe(skip);
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main');
  });

  test('exposes the navigation, banner and main landmarks', () => {
    renderShell('driver');

    expect(screen.getByRole('navigation', { name: en.common.nav.label })).toBeInTheDocument();
    expect(screen.getByRole('banner')).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveTextContent('page body');
  });

  test('guest shows Home and the footer inside the navigation', () => {
    renderShell('guest');

    const nav = screen.getByRole('navigation', { name: en.common.nav.label });
    expect(within(nav).getAllByRole('link').map((l) => l.textContent)).toEqual([en.common.nav.home]);
    expect(within(nav).getByRole('button', { name: 'Footer action' })).toBeInTheDocument();
    expect(within(nav).getByRole('link', { name: en.common.nav.home })).toHaveAttribute('aria-current', 'page');
  });

  test('driver marks My vehicle as the current page', () => {
    usePathname.mockReturnValue('/vehicles/new');
    renderShell('driver');

    expect(screen.getByRole('link', { name: en.common.nav.vehicle })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: en.common.nav.home })).not.toHaveAttribute('aria-current');
    expect(screen.getByRole('link', { name: en.common.nav.profile })).toHaveAttribute('href', '/en/profile');
  });

  test('admin shows Administration and Simulator, never My vehicle', () => {
    renderShell('admin');

    expect(screen.getByRole('link', { name: en.common.nav.admin })).toHaveAttribute('href', '/en/admin');
    expect(screen.getByRole('link', { name: en.common.nav.simulator })).toHaveAttribute('href', '/en/admin/simulator');
    expect(screen.queryByRole('link', { name: en.common.nav.vehicle })).not.toBeInTheDocument();
  });

  test('the top bar shows the location and a greeting only when a name is given', () => {
    const { unmount } = renderShell('driver', { greetingName: 'Ana' });
    expect(within(screen.getByRole('banner')).getByText(en.common.location)).toBeInTheDocument();
    expect(screen.getByText('Hello, Ana')).toBeInTheDocument();
    unmount();

    renderShell('guest');
    expect(screen.queryByText(/Hello,/)).not.toBeInTheDocument();
  });

  test('the orange underline is hidden from assistive tech', () => {
    const { container } = renderShell('guest');

    const underline = container.querySelector('.bg-brand-orange');
    expect(underline).toHaveAttribute('aria-hidden', 'true');
  });

  test('the menu button toggles the sidebar, Escape closes it and returns focus', () => {
    renderShell('driver');

    const toggle = screen.getByRole('button', { name: en.common.nav.menu });
    const nav = screen.getByRole('navigation', { name: en.common.nav.label });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveAttribute('aria-controls', nav.id);

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');

    screen.getByRole('link', { name: en.common.nav.profile }).focus();
    fireEvent.keyDown(nav, { key: 'Escape' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveFocus();
  });

  test('choosing an item closes the menu', () => {
    renderShell('driver');
    const toggle = screen.getByRole('button', { name: en.common.nav.menu });

    fireEvent.click(toggle);
    const item = screen.getByRole('link', { name: en.common.nav.profile });
    item.addEventListener('click', (event) => event.preventDefault()); // jsdom cannot navigate
    fireEvent.click(item);

    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });
});
