'use client';

import { useTranslations } from 'next-intl';
import { useEffect, useRef, type ReactNode } from 'react';
import { Banner } from '@/components/ui/Banner';
import { PageHeader } from '@/components/ui/PageHeader';
import { SimulatorStoreProvider, useConsoleStore } from '@/providers/simulator-store-provider';
import { consoleMode, deriveControls, type ConsoleMode } from './controls';
import { NotConfiguredPanel } from './NotConfiguredPanel';
import { useSimulatorStatus } from './queries';
import { StartForm } from './StartForm';
import { StatusPanel } from './StatusPanel';
import { StopControls } from './StopControls';

type Translate = ReturnType<typeof useTranslations<'simulator'>>;

/** What the always-mounted live region says for each mode (nothing while loading). */
function announce(mode: ConsoleMode, t: Translate): string {
  switch (mode) {
    case 'loading':
      return '';
    case 'notConfigured':
      return t('notConfigured');
    case 'forbidden':
      return t('forbidden');
    case 'unreachable':
      return t('unreachable');
    case 'error':
      return t('serverError');
    case 'idle':
      return t('status.idle');
    case 'running':
      return t('status.running');
    case 'draining':
      return t('status.draining');
  }
}

function ConsoleBody({ lot }: { lot: ReactNode }) {
  const t = useTranslations('simulator');
  const query = useSimulatorStatus();
  const draining = useConsoleStore((state) => state.draining);
  const setDraining = useConsoleStore((state) => state.setDraining);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const { data, error } = query;

  // The drain is over once the simulator reports it is not running (M2B-45).
  const running = data?.status.running;
  useEffect(() => {
    if (running === false && draining) setDraining(false);
  }, [running, draining, setDraining]);

  const mode = consoleMode({ data, error: error ?? undefined }, draining);
  // The panel keeps the last known state while a later poll fails.
  const dataMode = data ? consoleMode({ data }, draining) : mode;
  const controls = deriveControls(data?.status, draining);
  const startReason = !data ? t('start.disabledLoading') : draining ? t('start.disabledDraining') : t('start.disabledRunning');
  const bannerText = mode === 'unreachable' ? t('unreachable') : mode === 'forbidden' ? t('forbidden') : mode === 'error' ? t('serverError') : null;
  const focusStatus = () => headingRef.current?.focus();

  return (
    <div>
      <PageHeader title={t('title')} action={<span className="text-sm text-ink-muted">{t('refreshNote')}</span>} />
      <p role="status" className="sr-only">
        {announce(mode, t)}
      </p>
      {mode === 'notConfigured' ? (
        <NotConfiguredPanel />
      ) : (
        <>
          {bannerText ? <Banner tone="error">{bannerText}</Banner> : null}
          <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,440px)_minmax(0,1fr)]">
            <div className="grid gap-4">
              <StatusPanel mode={dataMode} result={data} headingRef={headingRef} />
              <StartForm disabled={!data || !controls.canStart} reason={startReason} onStarted={focusStatus} />
              <StopControls canStopNow={controls.canStopNow} canDrain={controls.canDrain} onStopped={focusStatus} />
            </div>
            <div className="grid gap-4">{lot}</div>
          </div>
        </>
      )}
    </div>
  );
}

export function SimulatorConsole({ lot }: { lot: ReactNode }) {
  return (
    <SimulatorStoreProvider>
      <ConsoleBody lot={lot} />
    </SimulatorStoreProvider>
  );
}
