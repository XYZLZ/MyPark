import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { LegalUnavailable } from '@/features/auth/LegalUnavailable';
import { RegisterForm } from '@/features/auth/RegisterForm';
import { getLegalCurrent } from '@/lib/api/server';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('auth.register');
  return { title: t('title') };
}

// Sign-up records which legal versions were accepted, so without them there is no form.
export default async function RegisterPage() {
  const legal = await getLegalCurrent().catch(() => null);

  return legal ? <RegisterForm legal={legal} /> : <LegalUnavailable />;
}
