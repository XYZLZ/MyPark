'use client';

import { acceptedStatusSchema, parseEnvelope, verifySchema } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { useRef, useState } from 'react';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { useRouter } from '@/i18n/navigation';
import { apiFetch, parseNoContentBody } from '@/lib/api/client';
import { isUndisclosedOutcome, useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';
import { AuthField } from './AuthField';

const parseAccepted = parseEnvelope(acceptedStatusSchema);

/** A pasted code often carries spaces or a line break. */
const stripWhitespace = (value: unknown) => String(value).replace(/\s+/g, '');

export function VerifyForm() {
  const t = useTranslations('auth');
  const router = useRouter();
  const savedEmail = useAuthFlow((s) => s.email);
  const setEmail = useAuthFlow((s) => s.setEmail);
  const [sent, setSent] = useState(false);
  const [resending, setResending] = useState(false);
  const resendInFlight = useRef(false);

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, fail, submitting } = useApiForm({
    schema: verifySchema,
    defaultValues: { email: savedEmail, code: '' },
    submit: async (values, form) => {
      setSent(false);
      await apiFetch('/api/v1/auth/verify', parseNoContentBody, { method: 'POST', body: values });
      // Redundant entry (WCAG 3.3.7): Login opens with this email already filled in.
      setEmail(values.email);
      form.reset();
      router.push('/login');
    },
  });
  const { register } = form;

  async function resend() {
    if (resendInFlight.current || lock.locked) return;
    if (!(await form.trigger('email', { shouldFocus: true }))) return;

    resendInFlight.current = true;
    setResending(true);
    setSent(false);
    try {
      await apiFetch('/api/v1/auth/verify/resend', parseAccepted, {
        method: 'POST',
        body: { email: form.getValues('email') },
      });
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
    <AuthPanel title={t('verify.title')}>
      <p className="mb-4 text-ink-muted">{t('verify.intro')}</p>
      {sent ? <Banner tone="status">{t('verify.sent')}</Banner> : null}
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="email" label={t('fields.email')} error={fieldMessage('email')}>
          <input type="email" inputMode="email" autoComplete="email" spellCheck={false} className={inputClass} {...register('email')} />
        </AuthField>
        <AuthField name="code" label={t('fields.code')} error={fieldMessage('code')}>
          <input
            type="text"
            inputMode="numeric"
            autoComplete="one-time-code"
            spellCheck={false}
            className={inputClass}
            {...register('code', { setValueAs: stripWhitespace })}
          />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('verify.submitting')} disabled={lock.locked}>
          {t('verify.submit')}
        </Button>
      </form>
      <Button
        variant="quiet"
        className="mt-3 w-full"
        loading={resending}
        loadingLabel={t('verify.resending')}
        disabled={lock.locked}
        onClick={() => void resend()}
      >
        {t('verify.resend')}
      </Button>
    </AuthPanel>
  );
}
