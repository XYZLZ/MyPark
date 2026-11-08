import { Text, type TextProps } from 'react-native';

/*
 * Uniwind maps `--font-*` to one family name, so each weight is its own family (spec §9.4).
 * Class names are literal: Uniwind cannot see dynamically built ones.
 */
const WEIGHT_CLASS = {
  regular: 'font-barlow-regular',
  medium: 'font-barlow-medium',
  semibold: 'font-barlow-semibold',
  bold: 'font-barlow-bold',
  plate: 'font-plate',
} as const;

const VARIANT_CLASS = {
  display: 'text-3xl text-ink',
  title: 'text-xl text-ink',
  body: 'text-base text-ink',
  label: 'text-base text-ink',
  caption: 'text-sm text-ink-muted',
} as const;

export type AppTextWeight = keyof typeof WEIGHT_CLASS;
export type AppTextVariant = keyof typeof VARIANT_CLASS;

type Props = TextProps & { weight?: AppTextWeight; variant?: AppTextVariant };

/** Text that follows the user's font scale. Only plates cap it, so they stay legible (spec §11). */
export function AppText({ weight = 'regular', variant = 'body', className, maxFontSizeMultiplier, ...rest }: Props) {
  const classes = `${WEIGHT_CLASS[weight]} ${VARIANT_CLASS[variant]}${className ? ` ${className}` : ''}`;
  return (
    <Text
      className={classes}
      maxFontSizeMultiplier={maxFontSizeMultiplier ?? (weight === 'plate' ? 1.3 : undefined)}
      {...rest}
    />
  );
}
