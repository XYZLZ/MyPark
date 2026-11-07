import { render, screen } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import { describe, expect, test, vi } from 'vitest';
import type { ApiHealthState } from '@mypark/contracts';
import { getApiHealth } from '@/lib/api/server';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import { HealthStatus } from './HealthStatus';

const messages = { en, 'es-DO': esDO } as const;

function renderIn(locale: keyof typeof messages, state: ApiHealthState) {
  return render(
    <NextIntlClientProvider locale={locale} messages={messages[locale]}>
      <HealthStatus state={state} />
    </NextIntlClientProvider>,
  );
}

describe('HealthStatus', () => {
  test('renders healthy in en', () => {
    renderIn('en', 'healthy');
    expect(screen.getByRole('status')).toHaveTextContent('API: healthy');
  });

  test('renders healthy in es-DO', () => {
    renderIn('es-DO', 'healthy');
    expect(screen.getByRole('status')).toHaveTextContent('API: operativa');
  });

  test.each([
    ['en', 'not_ready', 'API: starting'],
    ['es-DO', 'not_ready', 'API: iniciando'],
    ['en', 'unavailable', 'API: unavailable'],
    ['es-DO', 'unavailable', 'API: no disponible'],
  ] as const)('renders %s %s as "%s"', (locale, state, text) => {
    renderIn(locale, state);
    expect(screen.getByRole('status')).toHaveTextContent(text);
  });
});

describe('getApiHealth', () => {
  function stubApi(fetchImpl: ReturnType<typeof vi.fn>) {
    vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
    vi.stubGlobal('fetch', fetchImpl);
  }

  test('renders unavailable when fetch rejects', async () => {
    stubApi(vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    expect(await getApiHealth()).toBe('unavailable');
  });

  test('returns unavailable when the request times out', async () => {
    stubApi(vi.fn().mockRejectedValue(new DOMException('signal timed out', 'TimeoutError')));
    expect(await getApiHealth()).toBe('unavailable');
  });

  test('returns healthy for a 200 readiness body, fetched without cache and with a timeout', async () => {
    const timeoutSpy = vi.spyOn(AbortSignal, 'timeout');
    const fetchMock = vi.fn().mockResolvedValue(
      Response.json({ data: { status: 'ready', checks: { database: 'ok', migrations: 'ok' } }, meta: { request_id: 'r1' } }),
    );
    stubApi(fetchMock);

    try {
      expect(await getApiHealth()).toBe('healthy');
      expect(fetchMock).toHaveBeenCalledWith(
        'http://api.test/readyz',
        expect.objectContaining({ cache: 'no-store', signal: expect.any(AbortSignal) }),
      );
      // Must exceed the API's 2s readiness check timeout.
      expect(timeoutSpy).toHaveBeenCalledWith(3000);
    } finally {
      timeoutSpy.mockRestore();
    }
  });

  test('returns not_ready for a 503 health.not_ready envelope', async () => {
    stubApi(
      vi.fn().mockResolvedValue(
        Response.json(
          { error: { code: 'health.not_ready', message: 'migrations behind', details: {} }, meta: { request_id: 'r1' } },
          { status: 503 },
        ),
      ),
    );
    expect(await getApiHealth()).toBe('not_ready');
  });

  test('returns unavailable for a non-JSON body', async () => {
    stubApi(vi.fn().mockResolvedValue(new Response('<html>bad gateway</html>', { status: 502 })));
    expect(await getApiHealth()).toBe('unavailable');
  });

  test('returns unavailable when API_INTERNAL_URL is not set', async () => {
    const fetchMock = vi.fn();
    vi.stubEnv('API_INTERNAL_URL', '');
    vi.stubGlobal('fetch', fetchMock);

    expect(await getApiHealth()).toBe('unavailable');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
