import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { session } from '@/services/runtime';

/** Shown when the stored session could not be checked (offline, timeout, server trouble). The token is kept until Log out. */
export function RestoreFailedScreen() {
  const { t } = useTranslation();
  const [retrying, setRetrying] = useState(false);

  async function retry() {
    if (retrying) return;
    setRetrying(true);
    try {
      await session.restore();
    } finally {
      setRetrying(false);
    }
  }

  return (
    <ScreenScroll>
      <View className="gap-2 pt-8">
        <AppText accessibilityRole="header" weight="bold" variant="display">
          {t('restoreFailed.title')}
        </AppText>
        <AppText variant="body">{t('restoreFailed.body')}</AppText>
      </View>
      <Button variant="primary" label={t('common.retry')} loading={retrying} onPress={() => void retry()} />
      <Button variant="quiet" label={t('common.logOut')} disabled={retrying} onPress={() => void session.logout()} />
    </ScreenScroll>
  );
}
