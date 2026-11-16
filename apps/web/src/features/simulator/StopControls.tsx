'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useEffect, useRef } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { useConsoleStore } from '@/providers/simulator-store-provider';
import { translateError, type Translator } from '@/lib/i18n/translate-error';
import { ApiError } from '@mypark/contracts';
import { simulatorKeys, stopScenario } from './api';

/** "Stop after cars leave" sets `draining` on 202 (M2B-45); "Stop now" stays enabled to escalate. */
export function StopControls({ canStopNow, canDrain }: { canStopNow: boolean; canDrain: boolean }) {
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
    },
  });

  useEffect(() => {
    if (stop.isError) bannerRef.current?.focus();
  }, [stop.isError, stop.failureCount, stop.submittedAt]);

  const busy = stop.isPending;
  const error = stop.error instanceof ApiError ? stop.error : stop.error ? new ApiError({ code: 'internal.unexpected', message: 'unknown' }) : null;

  return (
    <section aria-labelledby="sim-stop-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-stop-title" className="mb-3 text-lg font-bold text-ink">
        {t('title')}
      </h2>
      {error ? (
        <Banner tone="error" ref={bannerRef}>
          {translateError(tAll, error)}
        </Banner>
      ) : null}
      <div className="flex flex-wrap gap-3">
        <Button variant="danger" disabled={!canStopNow} loading={busy && stop.variables === false} onClick={() => stop.mutate(false)}>
          {t('now')}
        </Button>
        <Button variant="outline" disabled={!canDrain} loading={busy && stop.variables === true} onClick={() => stop.mutate(true)}>
          {t('drain')}
        </Button>
      </div>
      <p className="mt-3 text-sm text-ink-muted">{t('drainingHint')}</p>
    </section>
  );
}
