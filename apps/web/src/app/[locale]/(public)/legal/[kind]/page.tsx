import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { getTranslations } from 'next-intl/server';
import { LegalDocument, isLegalKind } from '@/features/legal/LegalDocument';

type Props = { params: Promise<{ kind: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { kind } = await params;
  if (!isLegalKind(kind)) return {};
  const t = await getTranslations(`legal.${kind}`);
  return { title: t('title') };
}

export default async function LegalPage({ params }: Props) {
  const { kind } = await params;
  if (!isLegalKind(kind)) notFound();

  return <LegalDocument kind={kind} />;
}
