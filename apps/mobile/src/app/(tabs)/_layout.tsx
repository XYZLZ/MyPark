import { Tabs } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { TabLabel } from '@/components/ui/TabLabel';
import { tokens } from '@/design/tokens';
import { StorageNotice } from '@/features/session/StorageNotice';

/** Signed in and clear of the gate: two tabs, each with its own stack. */
export default function TabsLayout() {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  return (
    <View className="flex-1 bg-surface">
      {/* The one-time "session not saved" note sits above both tabs. */}
      <View style={{ paddingTop: insets.top }}>
        <StorageNotice />
      </View>
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarStyle: { height: 64, backgroundColor: tokens.surface, borderTopColor: tokens.hairline },
          tabBarIcon: () => null,
          tabBarIconStyle: { display: 'none' },
        }}
      >
        <Tabs.Screen
          name="vehicles"
          options={{ title: t('nav.vehicles'), tabBarLabel: ({ focused }) => <TabLabel focused={focused} label={t('nav.vehicles')} /> }}
        />
        <Tabs.Screen
          name="profile"
          options={{ title: t('nav.profile'), tabBarLabel: ({ focused }) => <TabLabel focused={focused} label={t('nav.profile')} /> }}
        />
      </Tabs>
    </View>
  );
}
