'use client';

import { acceptLegalSchema, type LegalCurrent, type User } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { linkClass } from '@/features/auth/link-class';
import { DeleteAccountForm } from '@/features/profile/DeleteAccountForm';
import { LogoutButtons } from '@/features/profile/LogoutButtons';
import { Link, useRouter } from '@/i18n/navigation';
import { apiFetch, handleSessionError, parseNoContentBody } from '@/lib/api/client';
import { useApiForm } from '@/lib/forms/use-api-form';

/**
 * The blocking screen for outdated legal acceptance (decision 9). Accept, log out and
 * delete stay available; nothing else renders. `user` names who is being asked.
 */
export function LegalGate({ legal, user }: { legal: LegalCurrent; user: User }) {
  const t = useTranslations();
  const router = useRouter();

  const { bannerRef, serverError, onSubmit, submitting, lock } = useApiForm({
    schema: acceptLegalSchema,
    defaultValues: { terms_version: legal.terms.version, privacy_version: legal.privacy.version },
    // The versions come from props at submit time, so a re-read after a 409 is honored.
    submit: async () => {
      await apiFetch('/api/v1/me/legal-acceptance', parseNoContentBody, {
        method: 'POST',
        body: { terms_version: legal.terms.version, privacy_version: legal.privacy.version },
      });
      router.refresh();
    },
    onError: (error) => {
      // Re-read the versions; the banner asks the user to accept again.
      if (error.code === 'legal.version_outdated') router.refresh();
      else handleSessionError(error, { refresh: () => router.refresh() });
      return false;
    },
  });

  return (
    <div className="flex flex-1 justify-center bg-brand-primary-strong px-4 py-8">
      <div className="flex w-full max-w-xl flex-col gap-5">
        <section aria-labelledby="gate-title" className="rounded-panel bg-surface p-6">
          <h1 id="gate-title" className="mb-2 text-2xl font-bold text-ink">
            {t('legalGate.title')}
          </h1>
          <p className="mb-1 text-ink-muted">{user.first_name},</p>
          <p className="mb-4 text-ink-muted">{t('legalGate.body')}</p>
          <ul className="mb-4 flex flex-col gap-1">
            {(['terms', 'privacy'] as const).map((kind) => (
              <li key={kind}>
                <Link href={`/legal/${kind}`} className={linkClass}>
                  {t(`legalGate.${kind}`)}
                </Link>{' '}
                <span className="text-sm text-ink-muted">{t('legalGate.version', { version: legal[kind].version })}</span>
              </li>
            ))}
          </ul>
          {serverError ? (
            <Banner tone="error" ref={bannerRef}>
              {serverError}
            </Banner>
          ) : null}
          <form noValidate onSubmit={onSubmit}>
            <Button type="submit" loading={submitting} loadingLabel={t('legalGate.accepting')} disabled={lock.locked}>
              {t('legalGate.accept')}
            </Button>
          </form>
        </section>

        <section aria-labelledby="gate-other" className="flex flex-col items-start gap-2 rounded-panel bg-surface p-6">
          <h2 id="gate-other" className="text-lg font-bold text-ink">
            {t('legalGate.otherTitle')}
          </h2>
          <p className="text-ink-muted">{t('legalGate.otherBody')}</p>
          <LogoutButtons scope="this" />
        </section>

        <DeleteAccountForm />
      </div>
    </div>
  );
}
