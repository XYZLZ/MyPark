import type { Locale } from '@/i18n/routing';

/**
 * Formats integer cents as Dominican pesos (M2B-26). Some ICU builds print the
 * ISO code (`DOP`) instead of `RD$`; then `fallback` (the `money.dop` message)
 * wraps the amount, still grouped and with two decimals in the locale.
 */
export function formatMoney(cents: number, locale: Locale, fallback: (amount: string) => string): string {
  const amount = cents / 100;
  const text = new Intl.NumberFormat(locale, { style: 'currency', currency: 'DOP' }).format(amount);
  if (!text.includes('DOP')) return text;
  return fallback(
    new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(amount),
  );
}
