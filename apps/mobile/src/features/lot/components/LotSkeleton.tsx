import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { Skeleton } from '@/components/ui/Skeleton';

/** While the lot is being read. The header and the actions are already on screen. */
export function LotSkeleton() {
  const { t } = useTranslation();
  return (
    <View accessible accessibilityLabel={t('common.loading')} className="gap-3">
      <Skeleton className="h-36 w-full" />
      <Skeleton className="h-28 w-full" />
    </View>
  );
}
