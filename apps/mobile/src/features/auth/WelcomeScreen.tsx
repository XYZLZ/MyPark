import { Redirect, useRouter } from 'expo-router';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { HeroPlaceholder } from '@/features/lot/components/HeroPlaceholder';
import { LotHeader } from '@/features/lot/components/LotHeader';
import { LotLive } from '@/features/lot/LotLive';
import { changeLanguage } from '@/features/session/change-language';
import type { AppLanguage } from '@/lib/i18n';
import { useAuthStore } from '@/store/auth';

/**
 * First screen when signed out: the live lot with Log in and Sign up right under the count, and the language switch.
 * No carousel (spec §2.1). The actions stay available with the API down.
 */
export function WelcomeScreen() {
  const { t, i18n } = useTranslation();
  const router = useRouter();
  const notice = useAuthStore((s) => s.notice);
  const clearNotice = useAuthStore((s) => s.clearNotice);

  // The logout-all notice is shown once: leaving the screen consumes it.
  useEffect(() => (notice === 'logout_all_failed' ? clearNotice : undefined), [notice, clearNotice]);

  // A session that ended (or an admin account turned away) is explained on Login, with the email pre-filled.
  if (notice === 'session_ended' || notice === 'admin_web_only') return <Redirect href="/login" />;

  return (
    <ScreenScroll
      banner={
        <LotHeader
          trailing={<LanguageSwitch value={i18n.language as AppLanguage} onChange={(language) => void changeLanguage(language)} />}
        />
      }
    >
      {notice === 'logout_all_failed' ? <Banner tone="info">{t('session.logoutAllFailed')}</Banner> : null}
      <HeroPlaceholder tagline />
      <LotLive
        cardClassName="-mt-14"
        actions={
          <>
            <Button variant="primary" label={t('auth.welcome.logIn')} onPress={() => router.push('/login')} />
            <Button variant="outline" label={t('auth.welcome.signUp')} onPress={() => router.push('/register')} />
          </>
        }
      />
    </ScreenScroll>
  );
}
