import { Pressable } from 'react-native';

import { AppText } from './AppText';

type Props = { label: string; onPress: () => void };

/** An in-screen link: a 48 dp target that reads as a link to screen readers. */
export function TextLink({ label, onPress }: Props) {
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={label}
      onPress={onPress}
      className="min-h-12 justify-center focus:rounded-control focus:border-2 focus:border-brand-primary"
    >
      <AppText weight="medium" className="text-brand-primary underline">
        {label}
      </AppText>
    </Pressable>
  );
}
