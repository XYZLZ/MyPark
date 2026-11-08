import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { LoginForm } from '@/features/auth/LoginForm';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('auth.login');
  return { title: t('adminTitle') };
}

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

const first = (value: string | string[] | undefined) => (typeof value === 'string' ? value : undefined);

// Outside the (admin) guard on purpose: this is where an unauthenticated admin lands.
export default async function AdminLoginPage({ searchParams }: { searchParams: SearchParams }) {
  const { next, reason } = await searchParams;

  return <LoginForm heading="admin" next={first(next)} reason={first(reason)} />;
}
