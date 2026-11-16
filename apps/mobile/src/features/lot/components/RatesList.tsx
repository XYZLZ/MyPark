import type { Tariff } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { formatMoney } from '@/lib/format/money';
import type { AppLanguage } from '@/lib/i18n';

/** The six rate lines (M2B-25). The reservation fee stays hidden until M3. */
export function RatesList({ tariff }: { tariff: Tariff }) {
  const { t, i18n } = useTranslation();
  const language = i18n.language as AppLanguage;

  const money = (cents: number) => formatMoney(cents, language, (amount) => t('money.dop', { amount }));
  const minutes = (value: number) => new Intl.NumberFormat(language, { style: 'unit', unit: 'minute', unitDisplay: 'short' }).format(value);

  const rows: [label: string, value: string][] = [
    [t('tariff.firstHour'), money(tariff.first_hour_cents)],
    [t('tariff.additionalHour'), money(tariff.additional_hour_cents)],
    [t('tariff.dailyCap'), money(tariff.daily_cap_cents)],
    [t('tariff.grace'), minutes(tariff.exit_grace_minutes)],
    [t('tariff.postPayment'), minutes(tariff.post_payment_exit_minutes)],
    [t('tariff.lostTicket'), money(tariff.lost_ticket_cents)],
  ];

  return (
    <View className="gap-1 rounded-panel border border-hairline bg-surface p-4">
      <AppText accessibilityRole="header" weight="bold" variant="title" className="mb-2 text-brand-navy">
        {t('tariff.title')}
      </AppText>
      {rows.map(([label, value], index) => (
        <View
          key={label}
          accessible
          accessibilityLabel={`${label}: ${value}`}
          className={`flex-row items-center justify-between gap-3 py-2.5 ${index === 0 ? '' : 'border-t border-hairline'}`}
        >
          <AppText className="flex-1">{label}</AppText>
          <AppText weight="bold" className="text-brand-navy">
            {value}
          </AppText>
        </View>
      ))}
    </View>
  );
}
