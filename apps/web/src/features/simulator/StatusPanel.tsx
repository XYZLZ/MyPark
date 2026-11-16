'use client';

import { CONSOLE_SCENARIOS } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { useEffect, useState, type ReactNode, type Ref } from 'react';
import { Button } from '@/components/ui/Button';
import type { Locale } from '@/i18n/routing';
import { formatClock } from '@/lib/format/time';
import type { StatusResult } from './api';
import type { ConsoleMode } from './controls';

function formatDateTime(at: string, locale: Locale): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short', timeZone: 'America/Santo_Domingo' }).format(date);
}

function Row({ label, children, wide = false }: { label: string; children: ReactNode; wide?: boolean }) {
  return (
    <div className={wide ? 'col-span-2' : undefined}>
      <dt className="text-sm text-ink-muted">{label}</dt>
      <dd className="font-semibold text-ink">{children}</dd>
    </div>
  );
}

type CopyState = 'idle' | 'copied' | 'failed';

/** Remounted per seed (`key`), so a new seed never shows "Copied". The note is also read out by its own live region. */
function CopySeed({ seed }: { seed: string }) {
  const t = useTranslations('simulator.status');
  const [state, setState] = useState<CopyState>('idle');

  useEffect(() => {
    if (state === 'idle') return;
    const timer = setTimeout(() => setState('idle'), 2_000);
    return () => clearTimeout(timer);
  }, [state]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(seed);
      setState('copied');
    } catch {
      setState('failed');
    }
  }

  const note = state === 'copied' ? t('copied') : state === 'failed' ? t('copyFailed') : '';
  return (
    <>
      <Button variant="outline" className="h-9 px-4 text-sm" onClick={() => void copy()}>
        {state === 'copied' ? t('copied') : t('copySeed')}
      </Button>
      {state === 'failed' ? <span className="text-sm text-danger">{note}</span> : null}
      <span role="status" className="sr-only">
        {note}
      </span>
    </>
  );
}

/** `headingRef` lets the console move focus here after an action (WCAG 2.4.3). */
export function StatusPanel({ mode, result, headingRef }: { mode: ConsoleMode; result?: StatusResult; headingRef?: Ref<HTMLHeadingElement> }) {
  const t = useTranslations('simulator.status');
  const ts = useTranslations('simulator.scenario');
  const locale = useLocale() as Locale;
  const status = result?.status;
  const badge = mode === 'draining' ? t('draining') : mode === 'running' ? t('running') : mode === 'idle' ? t('idle') : null;
  const known = (CONSOLE_SCENARIOS as readonly string[]).includes(status?.scenario ?? '');

  return (
    <section aria-labelledby="sim-status-title" className="rounded-panel bg-surface p-5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 ref={headingRef} tabIndex={-1} id="sim-status-title" className="text-lg font-bold text-ink outline-none">
          {t('title')}
        </h2>
        {badge ? <span className="rounded-full bg-brand-sky px-3 py-1 text-sm font-semibold text-brand-navy">{badge}</span> : null}
      </div>
      {status && result ? (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
          <Row label={t('scenario')}>
            {status.scenario === '' ? t('none') : known ? ts(status.scenario as 'idle') : <code className="font-mono">{status.scenario}</code>}
          </Row>
          <Row label={t('speed')}>×{status.speed}</Row>
          <Row label={t('seed')} wide>
            <span className="flex items-center gap-3">
              <code className="font-mono">{result.rawSeed}</code>
              <CopySeed key={result.rawSeed} seed={result.rawSeed} />
            </span>
          </Row>
          <Row label={t('virtualTime')}>{formatClock(status.virtual_now, locale)}</Row>
          <Row label={t('carsInside')}>{new Intl.NumberFormat(locale).format(status.cars_inside)}</Row>
          <Row label={t('startedAt')} wide>
            {status.started_at ? formatDateTime(status.started_at, locale) : '—'}
          </Row>
          {status.reached_full ? (
            <p className="col-span-2 text-sm font-semibold text-status-full">{t('reachedFull')}</p>
          ) : null}
        </dl>
      ) : mode === 'loading' ? (
        <p className="text-ink-muted">{t('loading')}</p>
      ) : null}
    </section>
  );
}
