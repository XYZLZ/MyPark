import { Redirect, useRouter } from 'expo-router';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { ScrollView, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { ParkingSign } from '@/components/ui/ParkingSign';
import { changeLanguage } from '@/features/session/change-language';
import type { AppLanguage } from '@/lib/i18n';
import { useAuthStore } from '@/store/auth';

/** First screen when signed out: the sign, two actions and the language switch. No carousel (spec §2.1). */
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
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      className="flex-1 bg-brand-primary-strong"
      contentContainerClassName="flex-grow justify-center gap-6 px-4 py-8"
    >
      {notice === 'logout_all_failed' ? <Banner tone="info">{t('session.logoutAllFailed')}</Banner> : null}
      <View className="items-center gap-4">
        <ParkingSign size="lg" />
        <AppText accessibilityRole="header" weight="bold" variant="display" className="text-brand-primary-contrast">
          {t('common.appName')}
        </AppText>
        <AppText className="text-center text-brand-primary-contrast">{t('auth.welcome.tagline')}</AppText>
      </View>
      <View className="items-center">
        <LanguageSwitch value={i18n.language as AppLanguage} onChange={(language) => void changeLanguage(language)} />
      </View>
      <View className="gap-3">
        <Button variant="primary" label={t('auth.welcome.signUp')} onPress={() => router.push('/register')} />
        <Button variant="onDark" label={t('auth.welcome.logIn')} onPress={() => router.push('/login')} />
      </View>
    </ScrollView>
  );
}
