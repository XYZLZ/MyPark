import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { Banner } from '@/components/ui/Banner';
import { TextLink } from '@/components/ui/TextLink';
import { useAuthStore } from '@/store/auth';

/**
 * The one-time note that the session could not be saved on this device (spec 6.4). The session service raises it only on the
 * move to memory-only, so this renders whatever the store holds and clears it when dismissed.
 */
export function StorageNotice() {
  const { t } = useTranslation();
  const notice = useAuthStore((s) => s.notice);
  const clearNotice = useAuthStore((s) => s.clearNotice);

  if (notice !== 'storage_failed') return null;
  return (
    <View className="gap-1 bg-surface px-4 pt-3">
      <Banner tone="info">{t('session.storageFailed')}</Banner>
      <View className="items-start">
        <TextLink label={t('common.dismiss')} onPress={clearNotice} />
      </View>
    </View>
  );
}
