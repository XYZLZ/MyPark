'use client';

import { acceptInvitationSchema } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { AuthField } from '@/features/auth/AuthField';
import { useRouter } from '@/i18n/navigation';
import { apiFetch, parseNoContentBody } from '@/lib/api/client';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';

/** A pasted code often carries spaces or a line break. */
const stripWhitespace = (value: unknown) => String(value).replace(/\s+/g, '');

export function AcceptInvitationForm() {
  const t = useTranslations();
  const router = useRouter();
  const savedEmail = useAuthFlow((s) => s.email);
  const setEmail = useAuthFlow((s) => s.setEmail);

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: acceptInvitationSchema,
    defaultValues: { email: savedEmail, code: '', password: '' },
    submit: async (values, form) => {
      await apiFetch('/api/v1/auth/invitations/accept', parseNoContentBody, { method: 'POST', body: values });
      // Redundant entry (WCAG 3.3.7): admin login opens with this email. The password is not kept.
      setEmail(values.email);
      form.reset();
      router.push('/admin/login');
    },
  });
  const { register } = form;

  return (
    <AuthPanel title={t('acceptInvitation.title')}>
      <p className="mb-4 text-ink-muted">{t('acceptInvitation.intro')}</p>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="email" label={t('auth.fields.email')} error={fieldMessage('email')}>
          <input type="email" inputMode="email" autoComplete="email" spellCheck={false} className={inputClass} {...register('email')} />
        </AuthField>
        <AuthField name="code" label={t('auth.fields.code')} error={fieldMessage('code')}>
          <input
            type="text"
            inputMode="numeric"
            autoComplete="one-time-code"
            spellCheck={false}
            className={inputClass}
            {...register('code', { setValueAs: stripWhitespace })}
          />
        </AuthField>
        <AuthField name="password" label={t('auth.fields.password')} hint={t('auth.register.passwordHint')} error={fieldMessage('password')}>
          <input type="password" autoComplete="new-password" className={inputClass} {...register('password')} />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('acceptInvitation.submitting')} disabled={lock.locked}>
          {t('acceptInvitation.submit')}
        </Button>
      </form>
    </AuthPanel>
  );
}
