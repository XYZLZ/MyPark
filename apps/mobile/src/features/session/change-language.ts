import type { AppLanguage } from '@/lib/i18n/pickLanguage';
import { http } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';

/**
 * Applies and persists the language. When the user is signed in and past the legal gate the
 * choice is also sent to the account, best effort: a failed PATCH keeps the local change (spec 6.6).
 */
export async function changeLanguage(language: AppLanguage): Promise<void> {
  useSettingsStore.getState().setLanguage(language);
  const { status, legalPending } = useAuthStore.getState();
  if (status !== 'signedIn' || legalPending) return;
  try {
    await http.request({ method: 'PATCH', path: '/v1/me', body: { locale: language }, auth: true });
  } catch {
    // The account catches up the next time the user changes language.
  }
}
