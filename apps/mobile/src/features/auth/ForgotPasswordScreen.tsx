import { emailOnlySchema } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { TextLink } from '@/components/ui/TextLink';
import { isUndisclosedOutcome, useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlowStore } from '@/store/auth-flow';

import { forgotPassword } from './api';
import { FormField } from './FormField';

/** The route's header carries the title, so this screen only has the intro. */
export function ForgotPasswordScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const savedEmail = useAuthFlowStore((s) => s.email);
  const setEmail = useAuthFlowStore((s) => s.setEmail);
  const [sent, setSent] = useState(false);

  const { form, lock, serverError, onSubmit, submitting } = useApiForm({
    schema: emailOnlySchema,
    defaultValues: { email: savedEmail },
    submit: async (values, form) => {
      setSent(false);
      try {
        await forgotPassword(values.email);
      } catch (error) {
        // The same message whether or not the account exists (spec 7.2).
        if (!isUndisclosedOutcome(error)) throw error;
      }
      // The reset screen opens with this email already filled in.
      setEmail(values.email);
      form.reset();
      setSent(true);
    },
  });

  return (
    <ScreenScroll>
      <AppText>{t('auth.forgot.intro')}</AppText>
      {sent ? <Banner tone="info">{t('auth.forgot.sent')}</Banner> : null}
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <FormField
        control={form.control}
        name="email"
        label={t('auth.fields.email')}
        keyboardType="email-address"
        autoCapitalize="none"
        autoComplete="email"
        textContentType="emailAddress"
        spellCheck={false}
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="primary" label={t('auth.forgot.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
      <View className="items-start">
        <TextLink label={t('auth.forgot.haveCode')} onPress={() => router.push('/reset-password')} />
        <TextLink label={t('auth.forgot.back')} onPress={() => router.replace('/login')} />
      </View>
    </ScreenScroll>
  );
}
