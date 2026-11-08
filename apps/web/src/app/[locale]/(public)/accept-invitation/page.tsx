import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { AcceptInvitationForm } from '@/features/admin/AcceptInvitationForm';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('acceptInvitation');
  return { title: t('title') };
}

export default function AcceptInvitationPage() {
  return <AcceptInvitationForm />;
}
