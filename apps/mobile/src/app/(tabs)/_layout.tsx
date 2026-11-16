import { Tabs } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { TabIcon } from '@/components/ui/TabIcon';
import { TabLabel } from '@/components/ui/TabLabel';
import { tabBarStyle } from '@/design/layout';
import { tokens } from '@/design/tokens';
import { StorageNotice } from '@/features/session/StorageNotice';
import { TAB_ORDER } from '@/features/session/tabs';

// Open on Inicio, whatever deep link or restore put the user in the tabs (M2B-48).
export const unstable_settings = { initialRouteName: 'home' };

/** Signed in and clear of the gate: the tabs, each with its own stack, in `TAB_ORDER`. */
export default function TabsLayout() {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  return (
    <View className="flex-1 bg-surface">
      {/* The one-time "session not saved" note sits above both tabs. */}
      <StorageNotice />
      <Tabs
        screenOptions={{
          headerShown: false,
          // The bar bleeds to the edges and clears the home indicator; its content stays inside the gutter.
          tabBarStyle: { ...tabBarStyle(insets), backgroundColor: tokens.surface, borderTopColor: tokens.hairline },
        }}
      >
        {TAB_ORDER.map((name) => (
          <Tabs.Screen
            key={name}
            name={name}
            options={{
              title: t(`nav.${name}`),
              tabBarIcon: ({ focused }) => <TabIcon name={name} focused={focused} />,
              tabBarLabel: ({ focused }) => <TabLabel focused={focused} label={t(`nav.${name}`)} />,
            }}
          />
        ))}
      </Tabs>
    </View>
  );
}
