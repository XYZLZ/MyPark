'use client';

import { useTranslations } from 'next-intl';
import { useEffect, useRef, type ReactNode } from 'react';
import { Banner } from '@/components/ui/Banner';
import { PageHeader } from '@/components/ui/PageHeader';
import { SimulatorStoreProvider, useConsoleStore } from '@/providers/simulator-store-provider';
import { consoleMode, deriveControls, type ConsoleMode } from './controls';
import { EventLog } from './EventLog';
import { FaultForm } from './FaultForm';
import { HeldCars } from './HeldCars';
import { InjectForm } from './InjectForm';
import { NotConfiguredPanel } from './NotConfiguredPanel';
import { useSimulatorEvents, useSimulatorStatus } from './queries';
import { StartForm } from './StartForm';
import { StatusPanel } from './StatusPanel';
import { StopControls } from './StopControls';

type Translate = ReturnType<typeof useTranslations<'simulator'>>;

/**
 * What the always-mounted live region says for each mode. Nothing while loading, and nothing for the modes
 * whose banner is an alert: that banner speaks, and saying it twice is noise.
 */
function announce(mode: ConsoleMode, t: Translate): string {
  switch (mode) {
    case 'loading':
    case 'forbidden':
    case 'unreachable':
    case 'error':
      return '';
    case 'notConfigured':
      return t('notConfigured');
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
  const clearHeld = useConsoleStore((state) => state.clearHeld);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const notConfiguredFocus = useRef(false);
  const wasRunning = useRef<boolean | undefined>(undefined);
  const { data, error } = query;
  useSimulatorEvents();

  // The drain is over once the simulator reports it is not running (M2B-45).
  const running = data?.status.running;
  useEffect(() => {
    if (running === false && draining) setDraining(false);
  }, [running, draining, setDraining]);

  // Held cars end with the run, not with a new `started_at`: a takeover of an idle session keeps them (M2B-44).
  useEffect(() => {
    if (wasRunning.current === true && running === false) clearHeld();
    if (running !== undefined) wasRunning.current = running;
  }, [running, clearHeld]);

  const mode = consoleMode({ data, error: error ?? undefined }, draining);
  // The panel keeps the last known state while a later poll fails.
  const dataMode = data ? consoleMode({ data }, draining) : mode;
  const controls = deriveControls(data?.status, draining);
  const startReason = !data ? t('start.disabledLoading') : draining ? t('start.disabledDraining') : t('start.disabledRunning');
  const reasonFor = (allowed: boolean, notRunning: boolean) =>
    !data ? t('start.disabledLoading') : draining && !allowed ? t('start.disabledDraining') : notRunning ? t('stop.notRunning') : undefined;
  const bannerText = mode === 'unreachable' ? t('unreachable') : mode === 'forbidden' ? t('forbidden') : mode === 'error' ? t('serverError') : null;
  const focusStatus = () => headingRef.current?.focus();
  // A failed action can end in the not configured panel, which replaces the pressed button: land on its heading.
  const onNotConfigured = () => {
    notConfiguredFocus.current = true;
  };
  useEffect(() => {
    if (mode === 'notConfigured' && notConfiguredFocus.current) {
      notConfiguredFocus.current = false;
      headingRef.current?.focus();
    }
  }, [mode]);

  return (
    <div>
      <PageHeader title={t('title')} action={<span className="text-sm text-ink-muted">{t('refreshNote')}</span>} />
      <p role="status" className="sr-only">
        {announce(mode, t)}
      </p>
      {mode === 'notConfigured' ? (
        <NotConfiguredPanel headingRef={headingRef} />
      ) : (
        <>
          {bannerText ? <Banner tone="error">{bannerText}</Banner> : null}
          <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,440px)_minmax(0,1fr)]">
            <div className="grid gap-4">
              <StatusPanel mode={dataMode} result={data} headingRef={headingRef} />
              <StartForm disabled={!data || !controls.canStart} reason={startReason} onStarted={focusStatus} onNotConfigured={onNotConfigured} />
              <StopControls
                canStopNow={controls.canStopNow}
                canDrain={controls.canDrain}
                statusKnown={Boolean(data)}
                onStopped={focusStatus}
                onNotConfigured={onNotConfigured}
              />
              <InjectForm
                disabled={!controls.canInject}
                reason={reasonFor(controls.canInject, false)}
                onDone={focusStatus}
                onNotConfigured={onNotConfigured}
              />
              <HeldCars
                disabled={!controls.canLeave}
                reason={reasonFor(controls.canLeave, true)}
                onDone={focusStatus}
                onNotConfigured={onNotConfigured}
              />
              <FaultForm
                disabled={!controls.canFault}
                reason={reasonFor(controls.canFault, true)}
                onDone={focusStatus}
                onNotConfigured={onNotConfigured}
              />
            </div>
            <div className="grid gap-4">
              {lot}
              <EventLog />
            </div>
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
