'use client';

import { loginResultSchema, loginSchema, parseEnvelope, type User } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { useEffect, useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { Link, useRouter } from '@/i18n/navigation';
import { apiFetch } from '@/lib/api/client';
import { safeNext } from '@/lib/auth/safe-next';
import { hardNavigate } from '@/lib/browser/navigate';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';
import { AuthField } from './AuthField';
import { linkClass } from './link-class';

const parseLogin = parseEnvelope(loginResultSchema);

/** `user.locale` wins over the locale in the URL (FR-I18N-2). */
function homeFor(user: User): string {
  return user.role === 'admin' ? `/${user.locale}/admin` : `/${user.locale}/vehicles`;
}

export function LoginForm({ heading, next, reason }: { heading: 'driver' | 'admin'; next?: string; reason?: string }) {
  const t = useTranslations('auth');
  const router = useRouter();
  const savedEmail = useAuthFlow((s) => s.email);
  const setEmail = useAuthFlow((s) => s.setEmail);
  const clearFlow = useAuthFlow((s) => s.clear);
  const [prefilled] = useState(savedEmail !== '');

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: loginSchema,
    defaultValues: { email: savedEmail, password: '' },
    submit: async (values, form) => {
      const { user } = await apiFetch('/api/auth/login', parseLogin, {
        method: 'POST',
        body: { email: values.email, password: values.password },
      });
      clearFlow();
      form.reset();
      hardNavigate(safeNext(next) ?? homeFor(user));
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
  const { register, setFocus } = form;

  // Redundant entry (WCAG 3.3.7): the email is already known, so the password is next.
  useEffect(() => {
    if (prefilled) setFocus('password');
  }, [prefilled, setFocus]);

  return (
    <AuthPanel title={heading === 'admin' ? t('login.adminTitle') : t('login.title')}>
      {reason === 'session_expired' ? <Banner tone="status">{t('login.sessionEnded')}</Banner> : null}
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="email" label={t('fields.email')} error={fieldMessage('email')}>
          <input type="email" inputMode="email" autoComplete="email" spellCheck={false} className={inputClass} {...register('email')} />
        </AuthField>
        <AuthField name="password" label={t('fields.password')} error={fieldMessage('password')}>
          <input type="password" autoComplete="current-password" className={inputClass} {...register('password')} />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('login.submitting')} disabled={lock.locked}>
          {t('login.submit')}
        </Button>
      </form>
      <p className="mt-4 flex flex-col gap-1 text-sm">
        <Link href="/forgot-password" className={linkClass}>
          {t('login.forgot')}
        </Link>
        {heading === 'driver' ? (
          <Link href="/register" className={linkClass}>
            {t('login.register')}
          </Link>
        ) : null}
      </p>
    </AuthPanel>
  );
}
