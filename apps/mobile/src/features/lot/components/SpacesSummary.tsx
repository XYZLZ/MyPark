import { countSpaces, type SpaceState } from '@mypark/contracts';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Pressable, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

/**
 * What screen readers get instead of the cells (design §11): one sentence for all 280, the note on where the map comes from
 * (M2B-22), and a toggle that opens the free spaces as text.
 */
export function SpacesSummary({ spaces }: { spaces: SpaceState[] }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const { free, occupied, outOfService } = countSpaces(spaces);
  const freeCodes = spaces.filter((space) => space.state === 'free').map((space) => space.code);
  return (
    <View className="gap-2">
      <AppText variant="caption" className="text-center">
        {t('spaces.note')}
      </AppText>
      <AppText>{t('spaces.summary', { free, occupied, oos: outOfService })}</AppText>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={t('spaces.listFree')}
        accessibilityState={{ expanded: open }}
        onPress={() => setOpen((value) => !value)}
        className="min-h-12 justify-center self-start"
      >
        <AppText weight="semibold" className="text-brand-navy">
          {t('spaces.listFree')}
        </AppText>
      </Pressable>
      {open ? <AppText>{freeCodes.join(', ')}</AppText> : null}
    </View>
  );
}
