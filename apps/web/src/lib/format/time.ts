import type { Locale } from '@/i18n/routing';

/** Wall-clock time in Santo Domingo, whatever the machine's time zone (`10:47 a. m.`, `10:47 AM`). */
export function formatClock(at: string | number, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, {
    hour: 'numeric',
    minute: '2-digit',
    timeZone: 'America/Santo_Domingo',
  }).format(new Date(at));
}
