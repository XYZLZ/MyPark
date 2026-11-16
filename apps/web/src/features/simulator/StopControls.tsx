'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useEffect, useRef } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import type { Translator } from '@/lib/i18n/translate-error';
import { useConsoleStore } from '@/providers/simulator-store-provider';
import { failureMessage } from './action-support';
import { simulatorKeys, stopScenario } from './api';
import { isNotConfigured } from './controls';

/**
 * "Stop after cars leave" sets `draining` on 202 (M2B-45); "Stop now" stays enabled to escalate.
 * `onStopped` runs after a successful stop, so the console can move focus off a button that is now disabled.
 */
export function StopControls({
  canStopNow,
  canDrain,
  statusKnown,
  onStopped,
  onNotConfigured,
}: {
  canStopNow: boolean;
  canDrain: boolean;
  /** False until a status has been read, so the reason does not claim "no scenario is running" about an unknown state. */
  statusKnown: boolean;
  onStopped?: () => void;
  onNotConfigured?: () => void;
}) {
  const t = useTranslations('simulator.stop');
  const tAll: Translator = useTranslations();
  const queryClient = useQueryClient();
  const setDraining = useConsoleStore((state) => state.setDraining);
  const bannerRef = useRef<HTMLDivElement>(null);

  const stop = useMutation({
    mutationFn: stopScenario,
    onSuccess: async (_result, drain) => {
      if (drain) setDraining(true);
      await queryClient.invalidateQueries({ queryKey: simulatorKeys.status });
      onStopped?.();
    },
    // A missing configuration shows in the panel too once the status refetches.
    onError: (error) => {
      if (!isNotConfigured(error)) return;
      onNotConfigured?.();
      void queryClient.invalidateQueries({ queryKey: simulatorKeys.status });
    },
  });

  useEffect(() => {
    if (stop.isError) bannerRef.current?.focus();
  }, [stop.isError, stop.submittedAt]);

  const busy = stop.isPending;
  const failure = stop.error;
  const message = failure ? failureMessage(tAll, failure) : null;

  // The reason a button is disabled is text tied to it, not only a dimmed button.
  const reason = !statusKnown ? t('unknown') : !canStopNow ? t('notRunning') : !canDrain ? t('drainingHint') : null;

  return (
    <section aria-labelledby="sim-stop-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-stop-title" className="mb-3 text-lg font-bold text-ink">
        {t('title')}
      </h2>
      {message ? (
        <Banner tone="error" ref={bannerRef}>
          {message}
        </Banner>
      ) : null}
      <div className="flex flex-wrap gap-3">
        <Button
          variant="danger"
          disabled={!canStopNow}
          aria-describedby={!canStopNow ? 'sim-stop-reason' : undefined}
          loading={busy && stop.variables === false}
          onClick={() => stop.mutate(false)}
        >
          {t('now')}
        </Button>
        <Button
          variant="outline"
          disabled={!canDrain}
          aria-describedby={!canDrain ? 'sim-stop-reason' : undefined}
          loading={busy && stop.variables === true}
          onClick={() => stop.mutate(true)}
        >
          {t('drain')}
        </Button>
      </div>
      {reason ? (
        <p id="sim-stop-reason" className="mt-3 text-sm text-ink-muted">
          {reason}
        </p>
      ) : null}
    </section>
  );
}
