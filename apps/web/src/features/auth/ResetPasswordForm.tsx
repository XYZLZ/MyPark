'use client';

import { resetSchema } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { useRouter } from '@/i18n/navigation';
import { apiFetch, parseNoContentBody } from '@/lib/api/client';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';
import { AuthField } from './AuthField';

/** A pasted code often carries spaces or a line break. */
const stripWhitespace = (value: unknown) => String(value).replace(/\s+/g, '');

export function ResetPasswordForm() {
  const t = useTranslations('auth');
  const router = useRouter();
  const savedEmail = useAuthFlow((s) => s.email);
  const setEmail = useAuthFlow((s) => s.setEmail);

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: resetSchema,
    defaultValues: { email: savedEmail, code: '', new_password: '' },
    submit: async (values, form) => {
      await apiFetch('/api/v1/auth/password/reset', parseNoContentBody, { method: 'POST', body: values });
      // The reset revoked every session, so clear this browser's cookies too (spec 5.8).
      // If there was no session, or it is already gone, there is nothing to undo.
      await apiFetch('/api/v1/auth/logout', parseNoContentBody, { method: 'POST' }).catch(() => undefined);
      setEmail(values.email);
      form.reset();
      router.push('/login');
    },
  });
  const { register } = form;

  return (
    <AuthPanel title={t('reset.title')}>
      <p className="mb-4 text-ink-muted">{t('reset.intro')}</p>
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
        <AuthField name="new_password" label={t('fields.newPassword')} error={fieldMessage('new_password')}>
          <input type="password" autoComplete="new-password" className={inputClass} {...register('new_password')} />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('reset.submitting')} disabled={lock.locked}>
          {t('reset.submit')}
        </Button>
      </form>
    </AuthPanel>
  );
}
