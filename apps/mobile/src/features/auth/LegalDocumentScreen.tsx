import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';

const KINDS = ['terms', 'privacy'] as const;
type Kind = (typeof KINDS)[number];

function isKind(value: string): value is Kind {
  return KINDS.some((kind) => kind === value);
}

/** Bundled sample text, marked as an academic demo. It never opens the seeded URLs, which a phone cannot reach (CC-21). */
export function LegalDocumentScreen({ kind }: { kind: string }) {
  const { t } = useTranslation();
  const router = useRouter();

  return (
    <ScreenScroll>
      {isKind(kind) ? (
        <>
          <AppText accessibilityRole="header" weight="bold" variant="display">
            {t(`legal.${kind}.title`)}
          </AppText>
          <AppText variant="caption">{t('legal.demoNotice')}</AppText>
          <AppText>{t(`legal.${kind}.body`)}</AppText>
        </>
      ) : (
        <AppText>{t('errors.http.not_found')}</AppText>
      )}
      <Button variant="quiet" label={t('legal.back')} onPress={() => router.back()} />
    </ScreenScroll>
  );
}
