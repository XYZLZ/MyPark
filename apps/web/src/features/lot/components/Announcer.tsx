'use client';

import {
  planAnnouncement,
  type AnnounceMemo,
  type AvailabilityFigures,
  type ConnectionStatus,
  type LotStatus,
} from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { useEffect, useRef, useState } from 'react';
import type { Locale } from '@/i18n/routing';
import { formatClock } from '@/lib/format/time';

/**
 * After one sentence, the next waits at least this long so the first is not replaced before a
 * screen reader finishes it (a status change must not be overwritten by a due count a few ms later).
 */
const SENTENCE_GAP_MS = 3_000;

type AnnouncerProps = {
  figures: AvailabilityFigures;
  status: LotStatus;
  connection: ConnectionStatus;
  /** Client time of the last frame, for the stale sentence. Defaults to the time of the announcement. */
  lastUpdateAt?: number | null;
};

/**
 * The one polite live region of the block (design §11, M2B-22). Silent on mount; a status or
 * connection change is said at once, a count change at most every 30 s. `planAnnouncement` only
 * decides on a call, so this re-calls it when something is still pending: at the returned `dueAt`
 * for a held-back count, and after a short gap when a status or connection change is waiting.
 */
export function Announcer({ figures, status, connection, lastUpdateAt = null }: AnnouncerProps) {
  const t = useTranslations('availability');
  const locale = useLocale() as Locale;
  const [message, setMessage] = useState('');
  const memoRef = useRef<AnnounceMemo | null>(null);
  const lastSaidAtRef = useRef(Number.NEGATIVE_INFINITY);
  const lastUpdateRef = useRef(lastUpdateAt);
  const { available, effectiveCapacity } = figures;

  useEffect(() => {
    lastUpdateRef.current = lastUpdateAt;
  }, [lastUpdateAt]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;

    function sentence(say: 'status' | 'connection' | 'count', now: number): string | null {
      if (say === 'status') return t(`announce.${status}`);
      if (say === 'count') return t('announce.count', { count: available, total: effectiveCapacity });
      if (connection === 'live') return t('announce.live');
      if (connection === 'stale') return t('announce.stale', { time: formatClock(lastUpdateRef.current ?? now, locale) }).trim();
      return null; // `connecting` and `unavailable` happen before the first frame: nothing to say.
    }

    function check(): void {
      clearTimeout(timer);
      const now = Date.now();
      const step = planAnnouncement(memoRef.current, { available, status, connection }, now);
      // A status change is said at once. Anything else, whatever re-ran this (a prop, a timer), waits
      // out the gap after the last sentence, so it cannot replace one a screen reader is still reading.
      const quietUntil = lastSaidAtRef.current + SENTENCE_GAP_MS;
      if (step.say !== null && step.say !== 'status' && now < quietUntil) {
        timer = setTimeout(check, quietUntil - now);
        return;
      }
      memoRef.current = step.memo;
      const said = step.say === null ? null : sentence(step.say, now);
      if (said !== null) {
        lastSaidAtRef.current = now;
        setMessage(said);
      }

      const otherPending = step.memo.status !== status || step.memo.connection !== connection;
      const gap = said === null ? 0 : SENTENCE_GAP_MS;
      if (otherPending) timer = setTimeout(check, gap);
      else if (step.dueAt !== null) timer = setTimeout(check, Math.max(step.dueAt - now, gap));
    }

    check();
    return () => clearTimeout(timer);
  }, [available, effectiveCapacity, status, connection, locale, t]);

  return (
    <p role="status" className="sr-only">
      {message}
    </p>
  );
}
