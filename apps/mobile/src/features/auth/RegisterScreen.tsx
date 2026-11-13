import { registerSchema, toRegisterRequest, type Locale } from '@mypark/contracts';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';
import { Controller } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Checkbox } from '@/components/ui/Checkbox';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { TextLink } from '@/components/ui/TextLink';
import { useApiForm } from '@/lib/forms/use-api-form';
import { translateFieldMessage } from '@/lib/i18n/translate-error';
import { useAuthFlowStore } from '@/store/auth-flow';

import { fetchLegal, register } from './api';
import { FormField } from './FormField';

export function RegisterScreen() {
  const { t, i18n } = useTranslation();
  const router = useRouter();
  const setEmail = useAuthFlowStore((s) => s.setEmail);
  // No versions means no sign-up: the user accepts what the server says is current.
  const legal = useQuery({ queryKey: ['legal', 'current'], queryFn: fetchLegal, retry: false });
  const { refetch } = legal;

  const { form, lock, serverError, onSubmit, submitting } = useApiForm({
    schema: registerSchema,
    defaultValues: {
      first_name: '',
      last_name: '',
      email: '',
      phone: '+1',
      password: '',
      // Starts unchecked; the schema only accepts `true`.
      accept_legal: false as unknown as true,
    },
    submit: async (values, form) => {
      if (!legal.data) return;
      await register(toRegisterRequest(values, legal.data, i18n.language as Locale));
      // Only the email travels on to Verify; the password goes nowhere.
      setEmail(values.email);
      form.reset();
      router.push('/verify');
    },
    onError: (error, _values, form) => {
      if (error.code === 'legal.version_outdated') {
        // The versions are stale: reload them and ask for a fresh acceptance. The banner still shows.
        form.setValue('accept_legal', false as unknown as true);
        void refetch();
      }
      return false;
    },
  });
  const { control, setFocus } = form;

  if (legal.isError) {
    return (
      <ScreenScroll>
        <AppText accessibilityRole="header" weight="bold" variant="display">
          {t('auth.register.unavailable.title')}
        </AppText>
        <AppText>{t('auth.register.unavailable.body')}</AppText>
        <Button variant="primary" label={t('common.retry')} onPress={() => void refetch()} />
      </ScreenScroll>
    );
  }

  return (
    <ScreenScroll>
      <AppText accessibilityRole="header" weight="bold" variant="display">
        {t('auth.register.title')}
      </AppText>
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      {legal.data ? (
        <>
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
            onSubmitEditing={() => setFocus('email')}
          />
          <FormField
            control={control}
            name="email"
            label={t('auth.fields.email')}
            keyboardType="email-address"
            autoCapitalize="none"
            autoComplete="email"
            textContentType="emailAddress"
            spellCheck={false}
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
            returnKeyType="next"
            onSubmitEditing={() => setFocus('password')}
          />
          <FormField
            control={control}
            name="password"
            label={t('auth.fields.password')}
            hint={t('auth.register.passwordHint')}
            secureTextEntry
            autoComplete="new-password"
            textContentType="newPassword"
          />
          <Controller
            control={control}
            name="accept_legal"
            render={({ field, fieldState }) => (
              <Checkbox
                label={t('auth.register.accept')}
                checked={field.value === true}
                onChange={(checked) => field.onChange(checked)}
                error={fieldState.error?.message ? translateFieldMessage(i18n, fieldState.error.message) : undefined}
              />
            )}
          />
          <View className="items-start">
            <TextLink label={t('auth.register.readTerms')} onPress={() => router.push('/legal/terms')} />
            <TextLink label={t('auth.register.readPrivacy')} onPress={() => router.push('/legal/privacy')} />
          </View>
          <Button variant="primary" label={t('auth.register.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
        </>
      ) : (
        <AppText>{t('common.loading')}</AppText>
      )}
      <TextLink label={t('auth.register.haveAccount')} onPress={() => router.push('/login')} />
    </ScreenScroll>
  );
}
