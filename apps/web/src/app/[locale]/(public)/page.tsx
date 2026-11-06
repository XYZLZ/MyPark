import { connection } from 'next/server';
import { setRequestLocale } from 'next-intl/server';
import { HealthStatus } from '@/features/health/HealthStatus';
import { getApiHealth } from '@/lib/api/server';

type HomePageProps = { params: Promise<{ locale: string }> };

// Container: reads the API health on every request and hands it to the
// presentational HealthStatus.
export default async function HomePage({ params }: HomePageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  // Render per request: a build-time prerender would freeze a stale health.
  await connection();
  const state = await getApiHealth();

  return <HealthStatus state={state} />;
}
