'use client';

import { deleteMeSchema } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { useRouter } from '@/i18n/navigation';
import { apiFetch, handleSessionError, parseNoContentBody } from '@/lib/api/client';
import { hardNavigate } from '@/lib/browser/navigate';
import { useApiForm } from '@/lib/forms/use-api-form';
import { AuthField } from '@/features/auth/AuthField';

/** Danger zone (decision 14): consequences, the password, then back to the home page. */
export function DeleteAccountForm() {
  const t = useTranslations();
  const locale = useLocale();
  const router = useRouter();

  const { form, bannerRef, serverError, fieldMessage, onSubmit, submitting, lock } = useApiForm({
    schema: deleteMeSchema,
    defaultValues: { password: '' },
    submit: async (values, form) => {
      await apiFetch('/api/v1/me', parseNoContentBody, { method: 'DELETE', body: values });
      form.reset();
      hardNavigate(`/${locale}`);
    },
    // A wrong password is a 403 and shows on the field: it must never refresh or end the session.
    onError: (error) => {
      handleSessionError(error, { refresh: () => router.refresh() });
      return false;
    },
  });
  const { register } = form;

  return (
    <section aria-labelledby="danger-title" className="rounded-panel border border-danger/40 bg-surface p-5">
      <h2 id="danger-title" className="mb-2 text-lg font-bold text-ink">
        {t('profile.danger.title')}
      </h2>
      <p className="mb-4 text-ink-muted">{t('profile.danger.body')}</p>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="password" label={t('auth.fields.password')} error={fieldMessage('password')}>
          <input type="password" autoComplete="current-password" className={inputClass} {...register('password')} />
        </AuthField>
        <Button type="submit" variant="danger" loading={submitting} loadingLabel={t('profile.danger.submitting')} disabled={lock.locked}>
          {t('profile.danger.submit')}
        </Button>
      </form>
    </section>
  );
}
