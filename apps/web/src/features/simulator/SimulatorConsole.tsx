'use client';

import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { Banner } from '@/components/ui/Banner';
import { PageHeader } from '@/components/ui/PageHeader';
import { SimulatorStoreProvider, useConsoleStore } from '@/providers/simulator-store-provider';
import { consoleMode, deriveControls } from './controls';
import { NotConfiguredPanel } from './NotConfiguredPanel';
import { useSimulatorStatus } from './queries';
import { StartForm } from './StartForm';
import { StatusPanel } from './StatusPanel';
import { StopControls } from './StopControls';

function ConsoleBody({ lot }: { lot: ReactNode }) {
  const t = useTranslations('simulator');
  const query = useSimulatorStatus();
  const draining = useConsoleStore((state) => state.draining);
  const setDraining = useConsoleStore((state) => state.setDraining);
  const { data, error } = query;

  // The drain is over once the simulator reports it is not running (M2B-45).
  const running = data?.status.running;
  useEffect(() => {
    if (running === false) setDraining(false);
  }, [running, setDraining]);

  const mode = consoleMode({ data, error: error ?? undefined }, draining);
  const controls = deriveControls(data?.status, draining);
  const startReason = draining ? t('start.disabledDraining') : t('start.disabledRunning');

  return (
    <div>
      <PageHeader title={t('title')} action={<span className="text-sm text-ink-muted">{t('refreshNote')}</span>} />
      {mode === 'notConfigured' ? (
        <NotConfiguredPanel />
      ) : (
        <>
          <p role="status" className="sr-only">
            {mode === 'draining' ? t('status.draining') : mode === 'running' ? t('status.running') : mode === 'idle' && data ? t('status.idle') : ''}
          </p>
          {mode === 'unreachable' ? <Banner tone="error">{t('unreachable')}</Banner> : null}
          <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,440px)_minmax(0,1fr)]">
            <div className="grid gap-4">
              <StatusPanel mode={mode} result={data} />
              <StartForm disabled={!controls.canStart} reason={startReason} />
              <StopControls canStopNow={controls.canStopNow} canDrain={controls.canDrain} />
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
