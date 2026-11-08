import { useTranslations } from 'next-intl';
import { LogoutButtons } from '@/features/profile/LogoutButtons';

/** Shown instead of the page while the account is suspended (CC-27). Log out stays available. */
export function AccountBlocked() {
  const t = useTranslations('accountBlocked');

  return (
    <div className="mx-auto flex w-full max-w-xl flex-1 flex-col items-start gap-4 px-4 py-16">
      <h1 className="text-2xl font-bold text-balance">{t('title')}</h1>
      <p className="text-ink-muted">{t('body')}</p>
      <LogoutButtons scope="this" />
    </div>
  );
}
