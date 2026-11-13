import { updateMeSchema, type User } from '@mypark/contracts';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'expo-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { Skeleton } from '@/components/ui/Skeleton';
import { TextField } from '@/components/ui/TextField';
import { TextLink } from '@/components/ui/TextLink';
import { FormField } from '@/features/auth/FormField';
import { changeLanguage } from '@/features/session/change-language';
import { LogoutButton } from '@/features/session/LogoutButton';
import type { AppLanguage } from '@/lib/i18n';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthStore } from '@/store/auth';

import { getMe, ME_QUERY_KEY, updateMe } from './api';

type Field = 'first_name' | 'last_name' | 'phone';
const FIELDS: Field[] = ['first_name', 'last_name', 'phone'];

const valuesOf = (user: User) => ({ first_name: user.first_name, last_name: user.last_name, phone: user.phone ?? '' });

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <View className="gap-3 border-t border-hairline pt-4">
      <AppText accessibilityRole="header" weight="bold" variant="title">
        {title}
      </AppText>
      {children}
    </View>
  );
}

function ProfileFields({ user }: { user: User }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(false);

  const { form, serverError, onSubmit, submitting, lock } = useApiForm({
    schema: updateMeSchema,
    defaultValues: valuesOf(user),
    submit: async (values, form) => {
      setSaved(false);
      // PATCH is partial: only what changed goes out, and an empty phone is what clears it.
      const current = valuesOf(user);
      const changes = Object.fromEntries(FIELDS.filter((name) => values[name] !== current[name]).map((name) => [name, values[name]]));
      if (Object.keys(changes).length > 0) {
        const updated = await updateMe(changes);
        form.reset(valuesOf(updated));
        queryClient.setQueryData(ME_QUERY_KEY, updated);
        useAuthStore.getState().setUser(updated);
      }
      setSaved(true);
    },
  });
  const { control, setFocus } = form;

  return (
    <View className="gap-4">
      {saved ? <Banner tone="info">{t('profile.saved')}</Banner> : null}
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <TextField label={t('auth.fields.email')} hint={t('profile.emailHint')} value={user.email} editable={false} />
      <FormField
        control={control}
        name="first_name"
        label={t('auth.fields.firstName')}
        autoComplete="given-name"
        textContentType="givenName"
        autoCapitalize="words"
        returnKeyType="next"
        onSubmitEditing={() => setFocus('last_name')}
      />
      <FormField
        control={control}
        name="last_name"
        label={t('auth.fields.lastName')}
        autoComplete="family-name"
        textContentType="familyName"
        autoCapitalize="words"
        returnKeyType="next"
        onSubmitEditing={() => setFocus('phone')}
      />
      <FormField
        control={control}
        name="phone"
        label={t('auth.fields.phone')}
        hint={t('auth.register.phoneHint')}
        keyboardType="phone-pad"
        autoComplete="tel"
        textContentType="telephoneNumber"
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="primary" label={t('profile.save')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
    </View>
  );
}

/** Account details, language, password, session and account deletion. */
export function ProfileScreen() {
  const { t, i18n } = useTranslation();
  const router = useRouter();
  const me = useQuery({ queryKey: ME_QUERY_KEY, queryFn: getMe });

  return (
    <ScreenScroll>
      {me.data ? (
        <ProfileFields key={me.data.id} user={me.data} />
      ) : me.isError ? (
        <>
          <Banner tone="error">{t('profile.loadError')}</Banner>
          <Button variant="primary" label={t('common.retry')} loading={me.isFetching} onPress={() => void me.refetch()} />
        </>
      ) : (
        <View accessible accessibilityRole="progressbar" accessibilityLabel={t('common.loading')} className="gap-3">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </View>
      )}

      <Section title={t('profile.languageTitle')}>
        <LanguageSwitch value={i18n.language as AppLanguage} onChange={(language) => void changeLanguage(language)} />
      </Section>

      <Section title={t('profile.password.title')}>
        <AppText>{t('profile.password.body')}</AppText>
        <View className="items-start">
          <TextLink label={t('profile.password.link')} onPress={() => router.push('/forgot-password')} />
        </View>
      </Section>

      <Section title={t('profile.sessionTitle')}>
        <LogoutButton scope="this" />
        <LogoutButton scope="all" />
      </Section>

      <Section title={t('profile.danger.title')}>
        <AppText>{t('profile.danger.body')}</AppText>
        <Button variant="danger" label={t('profile.danger.submit')} onPress={() => router.push('/profile/delete')} />
      </Section>
    </ScreenScroll>
  );
}
