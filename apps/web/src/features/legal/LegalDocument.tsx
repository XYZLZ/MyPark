import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { linkClass } from '@/features/auth/link-class';

const KINDS = ['terms', 'privacy'] as const;
export type LegalKind = (typeof KINDS)[number];

export function isLegalKind(value: string): value is LegalKind {
  return (KINDS as readonly string[]).includes(value);
}

/** Static legal text from the message files. It is sample text, and says so. */
export function LegalDocument({ kind }: { kind: LegalKind }) {
  const t = useTranslations('legal');

  return (
    <article className="mx-auto w-full max-w-prose px-4 py-8">
      <p className="mb-4 rounded-control border border-brand-primary/30 bg-brand-primary-tint p-3 text-sm">{t('demoNotice')}</p>
      <h1 className="mb-4 text-3xl font-bold text-ink">{t(`${kind}.title`)}</h1>
      <p className="text-ink">{t(`${kind}.body`)}</p>
      <p className="mt-8">
        <Link href="/" className={linkClass}>
          {t('backHome')}
        </Link>
      </p>
    </article>
  );
}
