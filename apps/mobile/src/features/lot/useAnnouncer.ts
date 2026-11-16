import { planAnnouncement, type AnnounceInput, type AnnounceMemo } from '@mypark/contracts';
import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { AccessibilityInfo } from 'react-native';

import { formatClock } from '@/lib/format/time';
import type { AppLanguage } from '@/lib/i18n';

/**
 * After one sentence, the next waits at least this long so the first is not cut off before a screen reader finishes it
 * (a status change must not be overwritten by a due count a few ms later).
 */
const SENTENCE_GAP_MS = 3_000;

type AnnounceDetails = {
  /** The total in "{count} of {total}". */
  total: number;
  /** Client time of the last frame, for the stale sentence. Defaults to the time of the announcement. */
  lastUpdateAt?: number | null;
};

/**
 * The polite announcement of the live block (design §11, M2B-22). Silent on mount; a status or connection change is said at
 * once, a count change at most every 30 s. `planAnnouncement` only decides on a call, so this re-calls it when something is
 * still pending: at the returned `dueAt` for a held-back count, and after a short gap when a status or connection change is
 * waiting. Same semantics as web's `Announcer`; the sentence goes to the screen reader instead of a live region.
 */
export function useAnnouncer(input: AnnounceInput | null, details: AnnounceDetails = { total: 0 }): void {
  const { t, i18n } = useTranslation();
  const language = i18n.language as AppLanguage;
  const memoRef = useRef<AnnounceMemo | null>(null);
  const lastSaidAtRef = useRef(Number.NEGATIVE_INFINITY);
  const lastUpdateRef = useRef(details.lastUpdateAt ?? null);
  const { available, status, connection } = input ?? { available: 0, status: 'open', connection: 'connecting' };
  const present = input !== null;
  const { total } = details;

  useEffect(() => {
    lastUpdateRef.current = details.lastUpdateAt ?? null;
  }, [details.lastUpdateAt]);

  useEffect(() => {
    if (!present) return;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function sentence(say: 'status' | 'connection' | 'count', now: number): string | null {
      if (say === 'status') return t(`availability.announce.${status}`);
      if (say === 'count') return t('availability.announce.count', { count: available, total });
      if (connection === 'live') return t('availability.announce.live');
      if (connection === 'stale') {
        return t('availability.announce.stale', { time: formatClock(lastUpdateRef.current ?? now, language) }).trim();
      }
      return null; // `connecting` and `unavailable` happen before the first frame: nothing to say.
    }

    function check(): void {
      clearTimeout(timer);
      const now = Date.now();
      const step = planAnnouncement(memoRef.current, { available, status, connection }, now);
      // A status change is said at once. Anything else, whatever re-ran this (a prop, a timer), waits out the gap after
      // the last sentence, so it cannot replace one a screen reader is still reading.
      const quietUntil = lastSaidAtRef.current + SENTENCE_GAP_MS;
      if (step.say !== null && step.say !== 'status' && now < quietUntil) {
        timer = setTimeout(check, quietUntil - now);
        return;
      }
      memoRef.current = step.memo;
      const said = step.say === null ? null : sentence(step.say, now);
      if (said !== null) {
        lastSaidAtRef.current = now;
        AccessibilityInfo.announceForAccessibility(said);
      }

      const otherPending = step.memo.status !== status || step.memo.connection !== connection;
      const gap = said === null ? 0 : SENTENCE_GAP_MS;
      if (otherPending) timer = setTimeout(check, gap);
      else if (step.dueAt !== null) timer = setTimeout(check, Math.max(step.dueAt - now, gap));
    }

    check();
    return () => clearTimeout(timer);
  }, [present, available, total, status, connection, language, t]);
}
