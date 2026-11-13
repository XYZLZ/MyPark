import { View } from 'react-native';

import { AppText } from './AppText';

type Props = { label: string; focused: boolean };

/** A tab's label with the active pill. The tab itself is the control (48 dp, from the tab bar), so this only draws. */
export function TabLabel({ label, focused }: Props) {
  return (
    <View className={`min-h-10 min-w-24 items-center justify-center rounded-full px-4 ${focused ? 'bg-brand-primary-tint' : 'bg-transparent'}`}>
      <AppText weight={focused ? 'bold' : 'medium'} className={focused ? 'text-brand-primary' : 'text-ink-muted'}>
        {label}
      </AppText>
    </View>
  );
}
