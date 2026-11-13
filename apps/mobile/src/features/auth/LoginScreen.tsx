import { loginSchema } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { TextLink } from '@/components/ui/TextLink';
import { useApiForm } from '@/lib/forms/use-api-form';
import { session } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { useAuthFlowStore } from '@/store/auth-flow';

import { login } from './api';
import { FormField } from './FormField';

const NOTICE_KEY = { session_ended: 'session.ended', admin_web_only: 'session.adminWebOnly' } as const;

export function LoginScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const savedEmail = useAuthFlowStore((s) => s.email);
  const setEmail = useAuthFlowStore((s) => s.setEmail);
  const clearFlow = useAuthFlowStore((s) => s.clear);
  const notice = useAuthStore((s) => s.notice);
  const clearNotice = useAuthStore((s) => s.clearNotice);
  const [prefilled] = useState(savedEmail !== '');

  const { form, lock, serverError, onSubmit, submitting } = useApiForm({
    schema: loginSchema,
    defaultValues: { email: savedEmail, password: '' },
    submit: async (values, form) => {
      // A new attempt answers the old notice. An admin account raises its own again after this call.
      clearNotice();
      const tokens = await login(values);
      // The root layout moves to the next area as soon as the session is set.
      await session.applyTokens(tokens);
      clearFlow();
      form.reset();
    },
    onError: (error, values, form) => {
      if (error.code !== 'auth.email_not_verified') return false;
      // Only the email travels on; the password is discarded with the form.
      setEmail(values.email);
      form.reset();
      router.push('/verify');
      return true;
    },
  });
  const { control, setFocus } = form;

  // Redundant entry (WCAG 3.3.7): the email is already known, so the password is next.
  useEffect(() => {
    if (prefilled) setFocus('password');
  }, [prefilled, setFocus]);

  // A notice is shown once: leaving the screen consumes it.
  useEffect(() => clearNotice, [clearNotice]);

  const noticeKey = notice === 'session_ended' || notice === 'admin_web_only' ? NOTICE_KEY[notice] : null;

  return (
    <ScreenScroll>
      <AppText accessibilityRole="header" weight="bold" variant="display">
        {t('auth.login.title')}
      </AppText>
      {noticeKey ? <Banner tone="info">{t(noticeKey)}</Banner> : null}
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <FormField
        control={control}
        name="email"
        label={t('auth.fields.email')}
        keyboardType="email-address"
        autoCapitalize="none"
        autoComplete="email"
        textContentType="username"
        spellCheck={false}
        returnKeyType="next"
        onSubmitEditing={() => setFocus('password')}
      />
      <FormField
        control={control}
        name="password"
        label={t('auth.fields.password')}
        secureTextEntry
        autoComplete="current-password"
        textContentType="password"
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="primary" label={t('auth.login.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
      <View className="items-start">
        <TextLink label={t('auth.login.forgot')} onPress={() => router.push('/forgot-password')} />
        <TextLink label={t('auth.login.register')} onPress={() => router.push('/register')} />
      </View>
    </ScreenScroll>
  );
}
