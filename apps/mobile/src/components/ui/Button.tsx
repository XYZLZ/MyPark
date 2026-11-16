import { useEffect, useRef, useState, type ReactNode } from 'react';
import { AccessibilityInfo, ActivityIndicator, Animated, Pressable } from 'react-native';

import { tokens } from '@/design/tokens';

import { AppText } from './AppText';

// The maps mirror the web Button (apps/web/src/components/ui/Button.tsx, spec §9.5 "Same").
// Web hover has no touch equivalent, so the press state uses the same color. The focus border
// is per variant: on navy (onDark) a navy border would be invisible, so it widens in white.
const SURFACE_CLASS = {
  primary:
    'bg-brand-navy active:bg-brand-navy-deep focus:border-2 focus:border-surface focus:outline-2 focus:outline-brand-navy',
  outline: 'bg-surface border-2 border-brand-navy active:bg-brand-primary-tint focus:border-4',
  onDark: 'bg-transparent border-2 border-brand-primary-contrast active:bg-white/10 focus:border-4',
  quiet: 'bg-transparent active:bg-brand-primary-tint focus:border-2 focus:border-brand-navy',
  danger: 'bg-danger active:opacity-90 focus:border-2 focus:border-brand-navy',
} as const;

const TEXT_CLASS = {
  primary: 'text-brand-primary-contrast',
  outline: 'text-brand-navy',
  onDark: 'text-brand-primary-contrast',
  quiet: 'text-brand-primary',
  danger: 'text-brand-primary-contrast',
} as const;

// Web disabled is a flat fill; onDark keeps its outline, in the disabled ink color.
const DISABLED_SURFACE = {
  primary: 'bg-disabled-bg',
  outline: 'bg-disabled-bg border-2 border-disabled-ink',
  onDark: 'bg-disabled-bg border-2 border-disabled-ink',
  quiet: 'bg-disabled-bg',
  danger: 'bg-disabled-bg',
} as const;

// Spinner color per variant, the text color as on web (`border-current`). The `accent-` form is
// for non-style props; a literal token value works too.
const SPINNER_COLOR = {
  primary: tokens.brandPrimaryContrast,
  outline: tokens.brandNavy,
  onDark: tokens.brandPrimaryContrast,
  quiet: tokens.brandPrimary,
  danger: tokens.brandPrimaryContrast,
} as const;

const PRESS_SCALE = 0.98;
const PRESS_MS = parseInt(tokens.motionDuration, 10);

export type ButtonVariant = keyof typeof SURFACE_CLASS;

type Props = {
  variant: ButtonVariant;
  label: string;
  loading?: boolean;
  disabled?: boolean;
  onPress?: () => void;
  /** 'link' for a button that leaves the app (Maps). */
  role?: 'button' | 'link';
  hint?: string;
  /** Decorative; drawn before the label. */
  icon?: ReactNode;
};

export function Button({ variant, label, loading = false, disabled = false, onPress, role = 'button', hint, icon }: Props) {
  const [scale] = useState(() => new Animated.Value(1));
  const reduceMotion = useRef(false);

  useEffect(() => {
    let alive = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((on) => {
      if (alive) reduceMotion.current = on;
    });
    const sub = AccessibilityInfo.addEventListener('reduceMotionChanged', (on) => {
      reduceMotion.current = on;
    });
    return () => {
      alive = false;
      sub.remove();
    };
  }, []);

  // Uniwind's free tier has no transitions (CC-19), so the press effect is core Animated.
  function animateTo(toValue: number) {
    if (reduceMotion.current) return;
    Animated.timing(scale, { toValue, duration: PRESS_MS, useNativeDriver: true }).start();
  }

  // Loading stays pressable so focus is not lost, but it must never submit twice.
  const inert = disabled || loading;
  const surface = disabled ? DISABLED_SURFACE[variant] : SURFACE_CLASS[variant];
  const textColor = disabled ? 'text-disabled-ink' : TEXT_CLASS[variant];

  return (
    <Animated.View style={{ transform: [{ scale }] }}>
      <Pressable
        accessibilityRole={role}
        accessibilityLabel={label}
        accessibilityHint={hint}
        accessibilityState={{ disabled, busy: loading }}
        disabled={disabled}
        onPress={() => {
          if (!inert) onPress?.();
        }}
        onPressIn={() => animateTo(PRESS_SCALE)}
        onPressOut={() => animateTo(1)}
        className={`min-h-12 flex-row items-center justify-center gap-2 rounded-full px-6 py-3 ${surface}`}
      >
        {icon}
        {loading ? <ActivityIndicator testID="button-spinner" color={SPINNER_COLOR[variant]} /> : null}
        <AppText weight="semibold" className={textColor}>
          {label}
        </AppText>
      </Pressable>
    </Animated.View>
  );
}
