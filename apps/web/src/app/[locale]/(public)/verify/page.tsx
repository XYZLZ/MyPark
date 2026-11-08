import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { VerifyForm } from '@/features/auth/VerifyForm';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('auth.verify');
  return { title: t('title') };
}

export default function Page() {
  return <VerifyForm />;
}
