import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/Button';
import { session } from '@/services/runtime';

/**
 * Ends the session on this device or on all of them. The session service always signs out locally, so the screen is
 * replaced when it finishes; a failed log out everywhere leaves a notice on Welcome (spec 6.5).
 */
export function LogoutButton({ scope }: { scope: 'this' | 'all' }) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);

  async function logout() {
    // One request however fast the button is tapped.
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      await session.logout(scope);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return <Button variant="quiet" label={scope === 'all' ? t('session.logoutAll') : t('common.logOut')} loading={busy} onPress={() => void logout()} />;
}
