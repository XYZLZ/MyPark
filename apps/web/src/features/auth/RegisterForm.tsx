'use client';

import {
  parseEnvelope,
  registerAcceptedSchema,
  registerSchema,
  toRegisterRequest,
  type LegalCurrent,
  type Locale,
} from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import type { ReactNode } from 'react';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Checkbox } from '@/components/ui/Checkbox';
import { inputClass } from '@/components/ui/Field';
import { Link, useRouter } from '@/i18n/navigation';
import { apiFetch } from '@/lib/api/client';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useAuthFlow } from '@/providers/auth-flow-provider';
import { AuthField } from './AuthField';
import { linkClass } from './link-class';

const parseRegistered = parseEnvelope(registerAcceptedSchema);

export function RegisterForm({ legal }: { legal: LegalCurrent }) {
  const t = useTranslations('auth');
  const locale = useLocale() as Locale;
  const router = useRouter();
  const setEmail = useAuthFlow((s) => s.setEmail);

  const { form, lock, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: registerSchema,
    defaultValues: {
      first_name: '',
      last_name: '',
      email: '',
      phone: '+1',
      password: '',
      // The checkbox starts unchecked; the schema only accepts `true`.
      accept_legal: false as unknown as true,
    },
    submit: async (values, form) => {
      await apiFetch('/api/v1/auth/register', parseRegistered, {
        method: 'POST',
        body: toRegisterRequest(values, legal, locale),
      });
      // Only the email travels on to Verify; the password goes nowhere.
      setEmail(values.email);
      form.reset();
      router.push('/verify');
    },
    onError: (error, _values, form) => {
      if (error.code === 'legal.version_outdated') {
        // The versions in `legal` are stale: reload them from the server and ask for a fresh acceptance.
        form.setValue('accept_legal', false as unknown as true);
        router.refresh();
      }
      // Not handled here: the translated message still shows in the banner.
      return false;
    },
  });
  const { register } = form;

  const legalLink = (href: '/legal/terms' | '/legal/privacy') =>
    function LegalLink(chunks: ReactNode) {
      return (
        <Link href={href} target="_blank" rel="noopener" className="font-medium text-brand-primary underline underline-offset-2">
          {chunks}
        </Link>
      );
    };

  return (
    <AuthPanel title={t('register.title')}>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit}>
        <AuthField name="first_name" label={t('fields.firstName')} error={fieldMessage('first_name')}>
          <input type="text" autoComplete="given-name" className={inputClass} {...register('first_name')} />
        </AuthField>
        <AuthField name="last_name" label={t('fields.lastName')} error={fieldMessage('last_name')}>
          <input type="text" autoComplete="family-name" className={inputClass} {...register('last_name')} />
        </AuthField>
        <AuthField name="email" label={t('fields.email')} error={fieldMessage('email')}>
          <input type="email" inputMode="email" autoComplete="email" spellCheck={false} className={inputClass} {...register('email')} />
        </AuthField>
        <AuthField name="phone" label={t('fields.phone')} hint={t('register.phoneHint')} error={fieldMessage('phone')}>
          <input type="tel" inputMode="tel" autoComplete="tel" className={inputClass} {...register('phone')} />
        </AuthField>
        <AuthField name="password" label={t('fields.password')} hint={t('register.passwordHint')} error={fieldMessage('password')}>
          <input type="password" autoComplete="new-password" className={inputClass} {...register('password')} />
        </AuthField>
        <AuthField name="accept_legal" error={fieldMessage('accept_legal')}>
          <Checkbox
            label={t.rich('register.accept', {
              terms: legalLink('/legal/terms'),
              privacy: legalLink('/legal/privacy'),
            })}
            {...register('accept_legal')}
          />
        </AuthField>
        <Button type="submit" className="w-full" loading={submitting} loadingLabel={t('register.submitting')} disabled={lock.locked}>
          {t('register.submit')}
        </Button>
      </form>
      <p className="mt-4 text-sm text-ink-muted">
        {t('register.haveAccount')}{' '}
        <Link href="/login" className={linkClass}>
          {t('register.logIn')}
        </Link>
      </p>
    </AuthPanel>
  );
}
