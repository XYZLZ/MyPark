'use client';

import { acceptedStatusSchema, emailOnlySchema, parseEnvelope } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { useState } from 'react';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { Link } from '@/i18n/navigation';
import { apiFetch } from '@/lib/api/client';
import { isUndisclosedOutcome, useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';
import { AuthField } from './AuthField';
import { linkClass } from './link-class';

const parseAccepted = parseEnvelope(acceptedStatusSchema);

export function ForgotPasswordForm() {
  const t = useTranslations('auth');
  const savedEmail = useAuthFlow((s) => s.email);
  const setEmail = useAuthFlow((s) => s.setEmail);
  const [sent, setSent] = useState(false);

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: emailOnlySchema,
    defaultValues: { email: savedEmail },
    submit: async (values, form) => {
      setSent(false);
      try {
        await apiFetch('/api/v1/auth/password/forgot', parseAccepted, { method: 'POST', body: values });
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
  const { register } = form;

  return (
    <AuthPanel title={t('forgot.title')}>
      <p className="mb-4 text-ink-muted">{t('forgot.intro')}</p>
      {sent ? <Banner tone="status">{t('forgot.sent')}</Banner> : null}
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="email" label={t('fields.email')} error={fieldMessage('email')}>
          <input type="email" inputMode="email" autoComplete="email" spellCheck={false} className={inputClass} {...register('email')} />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('forgot.submitting')} disabled={lock.locked}>
          {t('forgot.submit')}
        </Button>
      </form>
      <p className="mt-4 flex flex-col gap-1 text-sm">
        <Link href="/reset-password" className={linkClass}>
          {t('forgot.haveCode')}
        </Link>
        <Link href="/login" className={linkClass}>
          {t('forgot.back')}
        </Link>
      </p>
    </AuthPanel>
  );
}
