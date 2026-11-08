import type { ComponentRef, Ref } from 'react';
import { TextInput, View, type TextInputProps } from 'react-native';

import { tokens } from '@/design/tokens';

import { AppText } from './AppText';

type Props = Omit<TextInputProps, 'accessibilityLabel' | 'accessibilityHint' | 'ref'> & {
  label: string;
  error?: string;
  hint?: string;
  ref?: Ref<ComponentRef<typeof TextInput>>;
};

export function TextField({ label, error, hint, ref, className, ...inputProps }: Props) {
  // The input's own label carries the error so it is read on focus, not only when it appears (spec §11).
  const accessibleLabel = error ? `${label}, ${error}` : label;
  const border = error ? 'border-danger' : 'border-control-line focus:border-brand-primary';
  return (
    <View className="gap-1">
      <AppText weight="medium" variant="label" importantForAccessibility="no" accessibilityElementsHidden>
        {label}
      </AppText>
      <TextInput
        ref={ref}
        accessibilityLabel={accessibleLabel}
        accessibilityHint={hint}
        placeholderTextColor={tokens.inkMuted}
        className={`min-h-12 rounded-control border-2 bg-surface px-3 py-2 font-barlow-regular text-base text-ink ${border}${className ? ` ${className}` : ''}`}
        {...inputProps}
      />
      {hint && !error ? <AppText variant="caption">{hint}</AppText> : null}
      {error ? (
        <AppText variant="caption" className="text-danger" accessibilityLiveRegion="polite">
          {error}
        </AppText>
      ) : null}
    </View>
  );
}
