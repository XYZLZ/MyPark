import { verifySchema } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { isUndisclosedOutcome, useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlowStore } from '@/store/auth-flow';

import { resendVerification, verify } from './api';
import { FormField, stripWhitespace } from './FormField';

export function VerifyScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const savedEmail = useAuthFlowStore((s) => s.email);
  const setEmail = useAuthFlowStore((s) => s.setEmail);
  const [sent, setSent] = useState(false);
  const [resending, setResending] = useState(false);
  const resendInFlight = useRef(false);

  const { form, lock, serverError, onSubmit, fail, submitting } = useApiForm({
    schema: verifySchema,
    defaultValues: { email: savedEmail, code: '' },
    submit: async (values, form) => {
      setSent(false);
      await verify(values);
      // Redundant entry (WCAG 3.3.7): Login opens with this email already filled in.
      setEmail(values.email);
      form.reset();
      router.replace('/login');
    },
  });
  const { control, setFocus } = form;

  async function resend() {
    if (resendInFlight.current || lock.locked) return;
    if (!(await form.trigger('email', { shouldFocus: true }))) return;

    resendInFlight.current = true;
    setResending(true);
    setSent(false);
    try {
      await resendVerification(form.getValues('email'));
      form.clearErrors('root.serverError');
      setSent(true);
    } catch (error) {
      if (isUndisclosedOutcome(error)) {
        form.clearErrors('root.serverError');
        setSent(true);
      } else {
        fail(error);
      }
    } finally {
      resendInFlight.current = false;
      setResending(false);
    }
  }

  return (
    <ScreenScroll>
      <AppText accessibilityRole="header" weight="bold" variant="display">
        {t('auth.verify.title')}
      </AppText>
      <AppText>{t('auth.verify.intro')}</AppText>
      {sent ? <Banner tone="info">{t('auth.verify.sent')}</Banner> : null}
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
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
        onSubmitEditing={() => setFocus('code')}
      />
      <FormField
        control={control}
        name="code"
        label={t('auth.fields.code')}
        keyboardType="number-pad"
        autoComplete="one-time-code"
        textContentType="oneTimeCode"
        spellCheck={false}
        clean={stripWhitespace}
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="primary" label={t('auth.verify.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
      <Button variant="quiet" label={t('auth.verify.resend')} loading={resending} disabled={lock.locked} onPress={() => void resend()} />
    </ScreenScroll>
  );
}
