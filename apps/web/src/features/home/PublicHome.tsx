import { useTranslations } from 'next-intl';
import { buttonClass } from '@/components/ui/Button';
import { ParkingSign } from '@/components/ui/ParkingSign';
import { Link } from '@/i18n/navigation';

/** Welcome on navy. Only the presence of a session decides which actions show (CC-24). */
export function PublicHome({ signedIn }: { signedIn: boolean }) {
  const t = useTranslations('home');

  return (
    <section className="flex flex-1 flex-col items-center justify-center gap-6 bg-brand-primary-strong px-6 py-16 text-center text-brand-primary-contrast">
      <ParkingSign size="lg" />
      <h1 className="text-3xl font-bold text-balance">{t('title')}</h1>
      <p className="max-w-prose text-balance">{signedIn ? t('signedInSubtitle') : t('subtitle')}</p>
      <div className="flex w-full max-w-xs flex-col gap-3">
        {signedIn ? (
          <Link href="/vehicles" className={buttonClass('primary')}>
            {t('vehicles')}
          </Link>
        ) : (
          <>
            <Link href="/login" className={buttonClass('primary')}>
              {t('logIn')}
            </Link>
            <Link href="/register" className={buttonClass('onDark')}>
              {t('signUp')}
            </Link>
          </>
        )}
      </div>
    </section>
  );
}
