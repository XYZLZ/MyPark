import { acceptLegalSchema, type LegalCurrent } from '@mypark/contracts';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { Skeleton } from '@/components/ui/Skeleton';
import { fetchLegal } from '@/features/auth/api';
import { acceptLegal } from '@/features/profile/api';
import { DeleteAccountForm } from '@/features/profile/DeleteAccountForm';
import { LogoutButton } from '@/features/session/LogoutButton';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthStore } from '@/store/auth';

const LEGAL_QUERY_KEY = ['legal', 'current'] as const;
const KINDS = ['terms', 'privacy'] as const;

function AcceptLegal({ legal, onOutdated }: { legal: LegalCurrent; onOutdated(): void }) {
  const { t } = useTranslation();

  const { lock, serverError, onSubmit, submitting } = useApiForm({
    schema: acceptLegalSchema,
    defaultValues: { terms_version: legal.terms.version, privacy_version: legal.privacy.version },
    // The versions come from props at submit time, so a re-read after a 409 is honored.
    submit: async () => {
      await acceptLegal({ terms_version: legal.terms.version, privacy_version: legal.privacy.version });
      // The root layout moves on to the app as soon as the gate flag clears.
      useAuthStore.getState().setLegalPending(false);
    },
    onError: (error) => {
      // Re-read the versions; the banner asks the user to accept again.
      if (error.code === 'legal.version_outdated') onOutdated();
      return false;
    },
  });

  return (
    <View className="gap-4">
      {KINDS.map((kind) => (
        <View key={kind} className="gap-1">
          <AppText weight="semibold">{t(`legal.${kind}.title`)}</AppText>
          <AppText variant="caption">{t('legalGate.version', { version: legal[kind].version })}</AppText>
          <AppText>{t(`legal.${kind}.body`)}</AppText>
        </View>
      ))}
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <Button variant="primary" label={t('legalGate.accept')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
    </View>
  );
}

/**
 * The blocking screen for outdated legal acceptance. Accept, log out and delete stay available and nothing else renders,
 * whether or not the current versions could be loaded.
 */
export function LegalGateScreen() {
  const { t } = useTranslation();
  const legal = useQuery({ queryKey: LEGAL_QUERY_KEY, queryFn: fetchLegal });

  return (
    <ScreenScroll>
      <AppText accessibilityRole="header" weight="bold" variant="display">
        {t('legalGate.title')}
      </AppText>
      <AppText>{t('legalGate.body')}</AppText>

      {legal.data ? (
        <AcceptLegal legal={legal.data} onOutdated={() => void legal.refetch()} />
      ) : legal.isError ? (
        <>
          <Banner tone="error">{t('legalGate.loadError')}</Banner>
          <Button variant="primary" label={t('common.retry')} loading={legal.isFetching} onPress={() => void legal.refetch()} />
        </>
      ) : (
        <View accessible accessibilityRole="progressbar" accessibilityLabel={t('common.loading')} className="gap-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-12 w-full" />
        </View>
      )}

      <View className="gap-3 border-t border-hairline pt-4">
        <AppText accessibilityRole="header" weight="bold" variant="title">
          {t('legalGate.otherTitle')}
        </AppText>
        <AppText>{t('legalGate.otherBody')}</AppText>
        <LogoutButton scope="this" />
      </View>

      <View className="border-t border-hairline pt-4">
        <DeleteAccountForm onDeleted={() => undefined} />
      </View>
    </ScreenScroll>
  );
}
