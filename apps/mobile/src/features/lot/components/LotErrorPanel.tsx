import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';

/** The live block when nothing can be shown (M2B-17). The screen around it, including the actions, keeps working. */
export function LotErrorPanel({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <View className="items-start gap-4 rounded-panel border border-hairline bg-surface p-4">
      <AppText accessibilityRole="header" weight="bold" variant="title" className="text-brand-navy">
        {t('lot.error.title')}
      </AppText>
      <Button variant="primary" label={t('lot.error.retry')} onPress={onRetry} />
    </View>
  );
}
