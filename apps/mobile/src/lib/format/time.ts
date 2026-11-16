import type { AppLanguage } from '@/lib/i18n';

/**
 * Wall-clock time in Santo Domingo, whatever the phone's time zone (`10:47 a. m.`, `10:47 AM`).
 * An invalid time gives `''`, so a bad value never crashes a render.
 */
export function formatClock(at: string | number, language: AppLanguage): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit', timeZone: 'America/Santo_Domingo' }).format(date);
}
