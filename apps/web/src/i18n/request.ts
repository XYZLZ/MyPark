import * as rootParams from 'next/root-params';
import { getRequestConfig } from 'next-intl/server';
import { resolveLocale } from './routing';

export default getRequestConfig(async ({ locale: override }) => {
  const locale = resolveLocale(override ?? (await rootParams.locale()));

  return {
    locale,
    messages: (await import(`./messages/${locale}.json`)).default,
  };
});
