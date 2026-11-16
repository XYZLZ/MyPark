'use client';

import { CONSOLE_SCENARIOS } from '@mypark/contracts';
import { useLocale, useMessages, useTranslations } from 'next-intl';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { Locale } from '@/i18n/routing';
import { formatClock, formatLogTime } from '@/lib/format/time';
import type { Translator } from '@/lib/i18n/translate-error';
import { useConsoleStore } from '@/providers/simulator-store-provider';
import type { LogRow } from './log-cursor';

/** New events are announced at most this often, so a busy scenario does not spam a screen reader (design 7.3). */
export const LOG_ANNOUNCE_INTERVAL_MS = 10_000;

const COLUMNS = 7;

/** True only when `errors.<reason>` is one message, so a reason like `gate` is never mistaken for a message. */
function hasErrorMessage(messages: unknown, reason: string): boolean {
  let node: unknown = (messages as { errors?: unknown }).errors;
  for (const part of reason.split('.')) {
    if (typeof node !== 'object' || node === null || !Object.hasOwn(node, part)) return false;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === 'string';
}

/** Events above the last announced row, newest first. A row object stays the same across polls, so identity marks what was seen. */
function countUnseen(rows: LogRow[], seen: LogRow | null): number {
  const stop = seen ? rows.indexOf(seen) : -1;
  return rows.slice(0, stop === -1 ? rows.length : stop).filter((row) => row.kind === 'event').length;
}

/** `seq` starts over after a simulator restart, so each row's key carries the number of restarts below it. */
function rowKeys(rows: LogRow[]): string[] {
  const keys: string[] = new Array<string>(rows.length);
  let epoch = 0;
  for (let i = rows.length - 1; i >= 0; i--) {
    const row = rows[i]!;
    if (row.kind === 'restart') epoch++;
    const id = row.kind === 'event' ? row.event.seq : row.kind === 'gap' ? row.from : row.kind === 'run' ? row.startedAt : row.before;
    keys[i] = `${epoch}:${row.kind}:${id}`;
  }
  return keys;
}

/** One polite sentence, at most every LOG_ANNOUNCE_INTERVAL_MS. The table is never a live region. */
function useNewEventsMessage(rows: LogRow[]): string {
  const t = useTranslations('simulator.log');
  const [message, setMessage] = useState('');
  const seen = useRef<LogRow | null>(null);
  const lastAt = useRef<number | null>(null);

  useEffect(() => {
    const count = countUnseen(rows, seen.current);
    if (count === 0) return;
    const wait = lastAt.current === null ? 0 : Math.max(0, lastAt.current + LOG_ANNOUNCE_INTERVAL_MS - Date.now());
    const timer = setTimeout(() => {
      seen.current = rows[0] ?? null;
      lastAt.current = Date.now();
      setMessage(t('newEvents', { count }));
    }, wait);
    return () => clearTimeout(timer);
  }, [rows, t]);

  return message;
}

function Note({ children }: { children: string }) {
  return (
    <tr className="bg-brand-sky text-ink">
      <td colSpan={COLUMNS} className="px-3 py-2 text-center text-sm">
        {children}
      </td>
    </tr>
  );
}

function EventRow({ row, locale, tAll, messages }: { row: Extract<LogRow, { kind: 'event' }>; locale: Locale; tAll: Translator; messages: unknown }) {
  const t = useTranslations('simulator.log');
  const { event } = row;
  const denied = event.result.includes('deny');
  const known = event.reason !== undefined && hasErrorMessage(messages, event.reason);
  return (
    <tr className={denied ? 'bg-danger-tint text-ink' : 'text-ink'}>
      <td className="px-3 py-2 tabular-nums">{event.seq}</td>
      <td className="px-3 py-2 tabular-nums">{formatLogTime(event.at, locale)}</td>
      <td className="px-3 py-2 font-mono">{event.device}</td>
      <td className="px-3 py-2 font-mono">{event.type}</td>
      <td className="px-3 py-2">
        {denied ? <span className="mr-1 rounded-full border border-danger bg-surface px-2 py-0.5 text-xs font-semibold">{t('denied')}</span> : null}
        <span className="font-mono">{event.result}</span>
      </td>
      <td className="px-3 py-2">
        {event.reason === undefined ? (
          '—'
        ) : known ? (
          <>
            {tAll(`errors.${event.reason}`)}
            <code className="block font-mono text-xs text-ink-muted">{event.reason}</code>
          </>
        ) : (
          <code className="font-mono">{event.reason}</code>
        )}
      </td>
      <td className="px-3 py-2 font-mono">{event.car_id ?? '—'}</td>
    </tr>
  );
}

/** The last 500 events, newest first (M2B-30, M2B-35). `at` is virtual time in Santo Domingo. */
export function EventLog() {
  const t = useTranslations('simulator.log');
  const tScenario = useTranslations('simulator.scenario');
  const tAll: Translator = useTranslations();
  const messages = useMessages();
  const locale = useLocale() as Locale;
  const rows = useConsoleStore((state) => state.log.rows);
  const announcement = useNewEventsMessage(rows);
  const keys = useMemo(() => rowKeys(rows), [rows]);

  return (
    <section className="rounded-panel bg-surface p-5">
      <h2 id="sim-log-title" className="mb-3 text-lg font-bold text-ink">
        {t('title')}
      </h2>
      <p role="status" className="sr-only">
        {announcement}
      </p>
      {rows.length === 0 ? (
        <p className="text-ink-muted">{t('empty')}</p>
      ) : (
        <div role="region" aria-labelledby="sim-log-title" tabIndex={0} className="max-h-[32rem] overflow-auto rounded-control outline-none focus-visible:shadow-[0_0_0_3px_var(--color-brand-navy)]">
          <table className="w-full min-w-[46rem] text-left text-sm">
            <caption className="pb-2 text-left text-sm text-ink-muted">{t('caption')}</caption>
            <thead className="text-ink-muted">
              <tr>
                {(['seq', 'time', 'device', 'type', 'result', 'reason', 'car'] as const).map((column) => (
                  <th key={column} scope="col" className="px-3 py-2 font-semibold">
                    {t(column)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => {
                const key = keys[index]!;
                switch (row.kind) {
                  case 'event':
                    return <EventRow key={key} row={row} locale={locale} tAll={tAll} messages={messages} />;
                  case 'gap':
                    return <Note key={key}>{t('gap', { from: row.from, to: row.to })}</Note>;
                  case 'restart':
                    return <Note key={key}>{t('restarted')}</Note>;
                  case 'run': {
                    const scenario = (CONSOLE_SCENARIOS as readonly string[]).includes(row.scenario) ? tScenario(row.scenario as 'idle') : row.scenario;
                    return <Note key={key}>{t('run', { scenario, time: formatClock(row.startedAt, locale) })}</Note>;
                  }
                }
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
