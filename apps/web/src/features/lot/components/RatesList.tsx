import type { Tariff } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import type { Locale } from '@/i18n/routing';
import { formatMoney } from '@/lib/format/money';
import { Icon } from './icons';

/** The six rate lines (M2B-25). The reservation fee stays hidden until M3. */
export function RatesList({ tariff }: { tariff: Tariff }) {
  const t = useTranslations('tariff');
  const tMoney = useTranslations('money');
  const locale = useLocale() as Locale;

  const money = (cents: number) => formatMoney(cents, locale, (amount) => tMoney('dop', { amount }));
  const minutes = (value: number) =>
    new Intl.NumberFormat(locale, { style: 'unit', unit: 'minute', unitDisplay: 'short' }).format(value);

  const rows: Array<[label: string, value: string]> = [
    [t('firstHour'), money(tariff.first_hour_cents)],
    [t('additionalHour'), money(tariff.additional_hour_cents)],
    [t('dailyCap'), money(tariff.daily_cap_cents)],
    [t('grace'), minutes(tariff.exit_grace_minutes)],
    [t('postPayment'), minutes(tariff.post_payment_exit_minutes)],
    [t('lostTicket'), money(tariff.lost_ticket_cents)],
  ];

  return (
    <section className="rounded-panel bg-surface px-5.5 py-5 shadow-sm">
      <h2 className="mb-3.5 flex items-center gap-2.5 text-xl font-bold text-brand-navy">
        <Icon name="tag" />
        {t('title')}
      </h2>
      <ul className="m-0 list-none p-0">
        {rows.map(([label, value]) => (
          <li key={label} className="flex justify-between gap-3 py-2.5 text-[0.9375rem] not-first:border-t not-first:border-hairline">
            {label}
            <b className="font-bold whitespace-nowrap text-brand-navy tabular-nums">{value}</b>
          </li>
        ))}
      </ul>
    </section>
  );
}
