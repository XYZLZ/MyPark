import type { Metadata } from 'next';
import { Barlow, Barlow_Semi_Condensed } from 'next/font/google';
import { notFound } from 'next/navigation';
import { hasLocale, NextIntlClientProvider } from 'next-intl';
import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { routing } from '@/i18n/routing';
import { AppProviders } from '@/providers/app-providers';
import '../globals.css';

// Barlow for everything, Barlow Semi Condensed 600 for the plate only (spec §9.4).
const barlow = Barlow({
  subsets: ['latin'],
  weight: ['400', '500', '600', '700'],
  variable: '--font-barlow',
  display: 'swap',
});
const barlowSemiCondensed = Barlow_Semi_Condensed({
  subsets: ['latin'],
  weight: '600',
  variable: '--font-barlow-semi-condensed',
  display: 'swap',
});

type LocaleLayoutProps = {
  children: ReactNode;
  params: Promise<{ locale: string }>;
};

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

export async function generateMetadata({ params }: Omit<LocaleLayoutProps, 'children'>): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'common' });
  return { title: t('appName') };
}

// The header is rendered by each route group layout, so this root layout never
// reads cookies and stays statically renderable.
export default async function LocaleLayout({ children, params }: LocaleLayoutProps) {
  const { locale } = await params;
  // The proxy redirects unsupported locales; this guards direct renders.
  if (!hasLocale(routing.locales, locale)) notFound();

  return (
    <html lang={locale} className={`${barlow.variable} ${barlowSemiCondensed.variable}`}>
      <body className="flex min-h-screen flex-col bg-surface-sunken font-sans text-ink antialiased">
        <NextIntlClientProvider>
          <AppProviders>{children}</AppProviders>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
