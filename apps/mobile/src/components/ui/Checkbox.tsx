import { Pressable, View } from 'react-native';

import { AppText } from './AppText';

type Props = {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  error?: string;
};

export function Checkbox({ label, checked, onChange, error }: Props) {
  return (
    <View className="gap-1">
      <Pressable
        accessibilityRole="checkbox"
        accessibilityLabel={error ? `${label}, ${error}` : label}
        accessibilityState={{ checked }}
        onPress={() => onChange(!checked)}
        className="min-h-12 flex-row items-center gap-3 py-2 focus:rounded-control focus:border-2 focus:border-brand-primary"
      >
        <View
          accessible={false}
          importantForAccessibility="no-hide-descendants"
          className={`size-6 items-center justify-center rounded-md border-2 ${checked ? 'border-brand-primary bg-brand-primary' : error ? 'border-danger bg-surface' : 'border-control-line bg-surface'}`}
        >
          {checked ? <AppText weight="bold" className="text-sm text-brand-primary-contrast">✓</AppText> : null}
        </View>
        <AppText className="flex-1">{label}</AppText>
      </Pressable>
      {error ? (
        <AppText variant="caption" className="text-danger" accessibilityLiveRegion="polite">
          {error}
        </AppText>
      ) : null}
    </View>
  );
}
