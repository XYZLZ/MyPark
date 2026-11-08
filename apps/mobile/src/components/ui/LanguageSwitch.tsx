import { Pressable, View } from 'react-native';

import type { AppLanguage } from '@/lib/i18n/pickLanguage';

import { AppText } from './AppText';

// Each language is named in itself, so it reads the same whichever language is active.
// `accessibilityLanguage` (iOS) makes VoiceOver pronounce each name with its own voice.
const OPTIONS = [
  { value: 'es-DO', label: 'Español', speech: 'es' },
  { value: 'en', label: 'English', speech: 'en' },
] as const satisfies readonly { value: AppLanguage; label: string; speech: string }[];

type Props = { value: AppLanguage; onChange: (language: AppLanguage) => void };

export function LanguageSwitch({ value, onChange }: Props) {
  return (
    <View className="flex-row gap-2">
      {OPTIONS.map((option) => {
        const selected = option.value === value;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="button"
            accessibilityLabel={option.label}
            accessibilityLanguage={option.speech}
            accessibilityState={{ selected }}
            onPress={() => onChange(option.value)}
            className={`min-h-12 min-w-12 items-center justify-center rounded-full border-2 px-4 focus:border-brand-primary ${selected ? 'border-brand-primary bg-brand-primary-tint' : 'border-control-line bg-surface'}`}
          >
            <AppText weight={selected ? 'bold' : 'regular'} className={selected ? 'text-brand-primary' : 'text-ink'}>
              {option.label}
            </AppText>
          </Pressable>
        );
      })}
    </View>
  );
}
