import { Tabs } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { TabIcon } from '@/components/ui/TabIcon';
import { TabLabel } from '@/components/ui/TabLabel';
import { tabBarStyle } from '@/design/layout';
import { tokens } from '@/design/tokens';
import { StorageNotice } from '@/features/session/StorageNotice';

/** Signed in and clear of the gate: two tabs, each with its own stack. */
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
        <Tabs.Screen
          name="vehicles"
          options={{
            title: t('nav.vehicles'),
            tabBarIcon: ({ focused }) => <TabIcon name="vehicles" focused={focused} />,
            tabBarLabel: ({ focused }) => <TabLabel focused={focused} label={t('nav.vehicles')} />,
          }}
        />
        <Tabs.Screen
          name="profile"
          options={{
            title: t('nav.profile'),
            tabBarIcon: ({ focused }) => <TabIcon name="profile" focused={focused} />,
            tabBarLabel: ({ focused }) => <TabLabel focused={focused} label={t('nav.profile')} />,
          }}
        />
      </Tabs>
    </View>
  );
}
