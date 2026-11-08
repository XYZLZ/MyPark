import { beforeEach, describe, expect, test, vi } from 'vitest';
import * as rootParams from 'next/root-params';
import type { GetRequestConfigParams } from 'next-intl/server';
import requestConfig from './request';

// `next/root-params` only works inside a Next.js request scope, so tests
// control the `[locale]` root param through this mock.
vi.mock('next/root-params', () => ({ locale: vi.fn() }));

// Outside a React Server Components bundle, `next-intl/server` resolves to a
// stub whose `getRequestConfig` throws. In the RSC bundle it returns the
// callback unchanged, so an identity mock exercises the real config function.
vi.mock('next-intl/server', () => ({
  getRequestConfig: <T>(createRequestConfig: T) => createRequestConfig,
}));

const rootLocale = vi.mocked(rootParams.locale);

function loadConfig(params: Partial<GetRequestConfigParams> = {}) {
  return requestConfig(params as GetRequestConfigParams);
}

describe('request config', () => {
  beforeEach(() => {
    rootLocale.mockReset();
  });

  test('uses root param locale', async () => {
    rootLocale.mockResolvedValue('en');

    const config = await loadConfig();

    expect(config.locale).toBe('en');
    expect((config.messages as { common: { appName: string } }).common.appName).toBe('MyPark · Banreservas Zona Colonial');
  });

  test('falls back to es-DO for unknown root param', async () => {
    rootLocale.mockResolvedValue('fr');

    const config = await loadConfig();

    expect(config.locale).toBe('es-DO');
  });

  test('falls back to es-DO when root param undefined', async () => {
    rootLocale.mockResolvedValue(undefined as unknown as string);

    const config = await loadConfig();

    expect(config.locale).toBe('es-DO');
  });

  test('explicit override wins', async () => {
    rootLocale.mockResolvedValue('es-DO');

    const config = await loadConfig({ locale: 'en' });

    expect(config.locale).toBe('en');
  });
});
