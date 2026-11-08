import type { User } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { LogoutButtons } from '@/features/profile/LogoutButtons';

/** Greeting and Log out only: no links to features that arrive in M4 (decision 3). */
export function AdminLanding({ user }: { user: User }) {
  const t = useTranslations('admin');

  return (
    <div className="flex flex-col items-start gap-4">
      <h1 className="text-2xl font-bold">{t('title')}</h1>
      <p>{t('greeting', { name: user.first_name })}</p>
      <p className="text-ink-muted">{t('body')}</p>
      <LogoutButtons scope="this" />
    </div>
  );
}
