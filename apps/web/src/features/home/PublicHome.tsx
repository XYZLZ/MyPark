import { useTranslations } from 'next-intl';
import type { ReactNode } from 'react';
import { Banner } from '@/components/ui/Banner';
import { buttonClass } from '@/components/ui/Button';
import { HomeHero } from '@/features/lot/components/HomeHero';
import { Link } from '@/i18n/navigation';

export type HomeNotice = 'logout_all_failed';

/**
 * The public home: the brand hero and the actions, then the lot section in a slot. The hero and
 * the actions never wait for the API (design §6.1); the slot is the lot counter, info and rates,
 * or its skeleton or error panel. Only the presence of a session decides which actions show (CC-24).
 */
export function PublicHome({ signedIn, notice, lot }: { signedIn: boolean; notice?: HomeNotice; lot: ReactNode }) {
  const t = useTranslations('home');
  const tSession = useTranslations('session');

  return (
    <div className="mx-auto grid w-full max-w-7xl content-start gap-5.5 px-4 py-6 sm:px-7 lg:grid-cols-[minmax(0,1.55fr)_minmax(18.75rem,1fr)]">
      {notice === 'logout_all_failed' ? (
        <div className="lg:col-span-full">
          <Banner tone="status">{tSession('logoutAllFailed')}</Banner>
        </div>
      ) : null}
      <div className="flex flex-col gap-4 lg:col-start-1 lg:row-start-1">
        <HomeHero />
        <div className="rounded-panel bg-surface p-5 shadow-sm">
          <p className="mb-4 max-w-prose text-balance">{signedIn ? t('signedInSubtitle') : t('subtitle')}</p>
          <div className="flex flex-col gap-3 sm:flex-row">
            {signedIn ? (
              <Link href="/vehicles" className={buttonClass('primary')}>
                {t('vehicles')}
              </Link>
            ) : (
              <>
                <Link href="/login" className={buttonClass('primary')}>
                  {t('logIn')}
                </Link>
                <Link href="/register" className={buttonClass('quiet')}>
                  {t('signUp')}
                </Link>
              </>
            )}
          </div>
        </div>
      </div>
      {lot}
    </div>
  );
}
