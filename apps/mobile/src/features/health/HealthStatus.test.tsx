import { render, screen } from '@testing-library/react-native';
import { I18nextProvider } from 'react-i18next';

import { initI18n, type AppLanguage } from '@/lib/i18n';
import type { ApiHealthState } from '@mypark/contracts';

import { HealthStatus } from './HealthStatus';

async function renderIn(lang: AppLanguage, state: ApiHealthState) {
  const i18n = initI18n(lang);
  await render(
    <I18nextProvider i18n={i18n}>
      <HealthStatus state={state} />
    </I18nextProvider>,
  );
}

test('renders healthy in es-DO', async () => {
  await renderIn('es-DO', 'healthy');
  expect(screen.getByText('API: operativa')).toBeTruthy();
});

test('renders healthy in en', async () => {
  await renderIn('en', 'healthy');
  expect(screen.getByText('API: healthy')).toBeTruthy();
});

test('renders unavailable and not_ready as text', async () => {
  await renderIn('es-DO', 'unavailable');
  expect(screen.getByText('API: no disponible')).toBeTruthy();
  await renderIn('en', 'not_ready');
  expect(screen.getByText('API: starting')).toBeTruthy();
});

test('announces status changes politely to assistive tech', async () => {
  await renderIn('en', 'unavailable');
  const status = screen.getByText('API: unavailable');
  expect(status.props.accessibilityRole).toBe('text');
  expect(status.props.accessibilityLiveRegion).toBe('polite');
});
