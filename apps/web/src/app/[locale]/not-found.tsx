import { useTranslations } from 'next-intl';
import { SiteHeader } from '@/components/SiteHeader';
import { buttonClass } from '@/components/ui/Button';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { Link } from '@/i18n/navigation';

// Rendered inside the locale layout (so messages are available) when a page
// calls notFound() or the catch-all route does.
export default function NotFound() {
  const common = useTranslations('common');
  const t = useTranslations('notFound');

  return (
    <>
      <SiteHeader appName={common('appName')} languageSwitch={<LanguageSwitch />} />
      <main id="main" className="mx-auto flex w-full max-w-xl flex-1 flex-col items-start gap-4 px-4 py-16">
        <h1 className="text-2xl font-bold text-balance">{t('title')}</h1>
        <p className="text-ink-muted">{t('body')}</p>
        <Link href="/" className={buttonClass('primary')}>
          {t('backHome')}
        </Link>
      </main>
    </>
  );
}
