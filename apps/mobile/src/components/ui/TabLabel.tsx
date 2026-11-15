import { View } from 'react-native';

import { AppText } from './AppText';

type Props = { label: string; focused: boolean };

/** A tab's label with a 3 dp navy underline when active. The tab itself is the control (48 dp, from the tab bar), so this only draws. */
export function TabLabel({ label, focused }: Props) {
  return (
    <View className="items-center">
      <AppText weight={focused ? 'bold' : 'medium'} className={focused ? 'text-brand-navy' : 'text-ink-muted'}>
        {label}
      </AppText>
      <View className={`mt-1 h-[3px] w-11 rounded-sm ${focused ? 'bg-brand-navy' : 'bg-transparent'}`} />
    </View>
  );
}
