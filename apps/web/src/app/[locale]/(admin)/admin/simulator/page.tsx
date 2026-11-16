import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { Suspense } from 'react';
import { LotSkeleton } from '@/features/lot/components/LotSkeleton';
import { LotSection } from '@/features/lot/LotSection';
import { RefreshOnMount } from '@/features/session/RefreshOnMount';
import { SimulatorConsole } from '@/features/simulator/SimulatorConsole';
import { resolveLocale } from '@/i18n/routing';
import { requireAdmin } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('simulator');
  return { title: t('title') };
}

// No loading.tsx next to this page: streaming would start before forbidden() runs (CC-7).
export default async function SimulatorPage() {
  const gate = await requireAdmin(resolveLocale(await getLocale()));
  if (gate.state !== 'ready') return <RefreshOnMount />;

  return (
    <SimulatorConsole
      lot={
        <Suspense fallback={<LotSkeleton />}>
          <LotSection variant="compact" />
        </Suspense>
      }
    />
  );
}
