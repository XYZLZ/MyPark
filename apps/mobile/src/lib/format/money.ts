import type { AppLanguage } from '@/lib/i18n';

/**
 * Formats integer cents as Dominican pesos (M2B-26). Some ICU builds print the ISO code (`DOP`) instead of `RD$`;
 * then `fallback` (the `money.dop` message) wraps the amount, still grouped and with two decimals in the language.
 */
export function formatMoney(cents: number, language: AppLanguage, fallback: (amount: string) => string): string {
  const amount = cents / 100;
  const text = new Intl.NumberFormat(language, { style: 'currency', currency: 'DOP' }).format(amount);
  if (!text.includes('DOP')) return text;
  return fallback(new Intl.NumberFormat(language, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(amount));
}
