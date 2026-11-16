import { View } from 'react-native';

type Props = { name: 'home' | 'vehicles' | 'profile'; focused: boolean };

/** A 24 dp icon drawn with Views, so the app needs no icon package. Silent to screen readers: the label names the tab. */
export function TabIcon({ name, focused }: Props) {
  const border = focused ? 'border-brand-navy' : 'border-ink-muted';
  const fill = focused ? 'bg-brand-navy' : 'bg-ink-muted';
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="h-6 w-6 items-center justify-center"
    >
      {name === 'home' ? (
        <>
          <View className={`h-2.5 w-2.5 rotate-45 border-l-2 border-t-2 ${border}`} />
          <View className={`-mt-0.5 h-2.5 w-4 rounded-b-sm border-2 ${border}`} />
        </>
      ) : null}
      {name === 'vehicles' ? (
        <>
          <View className={`h-2.5 w-4 rounded-t-md ${fill}`} />
          <View className={`h-2.5 w-6 rounded-md ${fill}`} />
        </>
      ) : null}
      {name === 'profile' ? (
        <>
          <View className={`h-2.5 w-2.5 rounded-full ${fill}`} />
          <View className={`mt-0.5 h-2.5 w-5 rounded-t-full ${fill}`} />
        </>
      ) : null}
    </View>
  );
}
