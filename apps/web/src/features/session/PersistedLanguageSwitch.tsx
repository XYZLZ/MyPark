'use client';

import { parseEnvelope, userSchema } from '@mypark/contracts';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { apiFetch } from '@/lib/api/client';

const parseUser = parseEnvelope(userSchema);

/**
 * The language switch for a signed-in user who is not behind the legal gate: it also
 * saves the choice (spec 5.8). PATCH is not legal-exempt, so the gate uses the plain switch.
 * Saving is best effort: the page language changes either way.
 */
export function PersistedLanguageSwitch() {
  return (
    <LanguageSwitch
      onSwitch={(locale) => {
        void apiFetch('/api/v1/me', parseUser, { method: 'PATCH', body: { locale } }).catch(() => undefined);
      }}
    />
  );
}
