'use client';

import { parseEnvelope, updateMeSchema, userSchema, type User } from '@mypark/contracts';
import { useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Field';
import { PageHeader } from '@/components/ui/PageHeader';
import { AuthField } from '@/features/auth/AuthField';
import { linkClass } from '@/features/auth/link-class';
import { Link, useRouter } from '@/i18n/navigation';
import { apiFetch, handleSessionError } from '@/lib/api/client';
import { useApiForm } from '@/lib/forms/use-api-form';
import { DeleteAccountForm } from './DeleteAccountForm';
import { LogoutButtons } from './LogoutButtons';

const parseUser = parseEnvelope(userSchema);
type Field = 'first_name' | 'last_name' | 'phone';
const FIELDS: Field[] = ['first_name', 'last_name', 'phone'];

const valuesOf = (user: User) => ({ first_name: user.first_name, last_name: user.last_name, phone: user.phone ?? '' });

export function ProfileForm({ user }: { user: User }) {
  const router = useRouter();
  const [saved, setSaved] = useState(false);

  const { form, t, bannerRef, serverError, fieldMessage, onSubmit, submitting, lock } = useApiForm({
    schema: updateMeSchema,
    defaultValues: valuesOf(user),
    submit: async (values, form) => {
      setSaved(false);
      // PATCH is partial: only what changed goes out, and an empty phone is what clears it.
      const current = valuesOf(user);
      const changes = Object.fromEntries(FIELDS.filter((name) => values[name] !== current[name]).map((name) => [name, values[name]]));
      if (Object.keys(changes).length > 0) {
        const updated = await apiFetch('/api/v1/me', parseUser, { method: 'PATCH', body: changes });
        form.reset(valuesOf(updated));
        router.refresh();
      }
      setSaved(true);
    },
    onError: (error) => {
      handleSessionError(error, { refresh: () => router.refresh() });
      return false;
    },
  });
  const { register } = form;

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6">
      <div>
        <PageHeader title={t('profile.title')} />
        {saved ? <Banner tone="status">{t('profile.saved')}</Banner> : null}
        {serverError ? (
          <Banner tone="error" ref={bannerRef}>
            {serverError}
          </Banner>
        ) : null}
        <form noValidate onSubmit={onSubmit} className="rounded-panel bg-surface p-5">
          <AuthField name="email" label={t('auth.fields.email')} hint={t('profile.emailHint')}>
            <input type="email" readOnly defaultValue={user.email} className={inputClass} />
          </AuthField>
          <AuthField name="first_name" label={t('auth.fields.firstName')} error={fieldMessage('first_name')}>
            <input type="text" autoComplete="given-name" className={inputClass} {...register('first_name')} />
          </AuthField>
          <AuthField name="last_name" label={t('auth.fields.lastName')} error={fieldMessage('last_name')}>
            <input type="text" autoComplete="family-name" className={inputClass} {...register('last_name')} />
          </AuthField>
          <AuthField name="phone" label={t('auth.fields.phone')} hint={t('auth.register.phoneHint')} error={fieldMessage('phone')}>
            <input type="tel" inputMode="tel" autoComplete="tel" className={inputClass} {...register('phone')} />
          </AuthField>
          <Button type="submit" loading={submitting} loadingLabel={t('profile.saving')} disabled={lock.locked}>
            {t('profile.save')}
          </Button>
        </form>
      </div>

      <section aria-labelledby="password-title" className="rounded-panel bg-surface p-5">
        <h2 id="password-title" className="mb-2 text-lg font-bold text-ink">
          {t('profile.password.title')}
        </h2>
        <p className="mb-2 text-ink-muted">{t('profile.password.body')}</p>
        <Link href="/forgot-password" className={linkClass}>
          {t('profile.password.link')}
        </Link>
      </section>

      <section aria-labelledby="session-title" className="flex flex-col items-start gap-2 rounded-panel bg-surface p-5">
        <h2 id="session-title" className="text-lg font-bold text-ink">
          {t('profile.sessionTitle')}
        </h2>
        <LogoutButtons scope="this" />
        <LogoutButtons scope="both" />
      </section>

      <DeleteAccountForm />
    </div>
  );
}
