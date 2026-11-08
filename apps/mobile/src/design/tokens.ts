/**
 * Design tokens for places that need a JS value (StatusBar, native props).
 * Same names and values as the @theme block in src/global.css and
 * apps/web/src/design/tokens.css; `npm run check:tokens` enforces it.
 * One flat `key: 'value'` per line: the script parses it.
 */
export const tokens = {
  /* TODO(brand): replace with official Banreservas value */
  brandPrimary: '#1c4a78',
  /* TODO(brand): replace with official Banreservas value */
  brandPrimaryStrong: '#163b63',
  /* TODO(brand): replace with official Banreservas value */
  brandPrimaryTint: '#e3eef8',
  /* TODO(brand): replace with official Banreservas value */
  brandPrimaryContrast: '#ffffff',
  /* TODO(brand): replace with official Banreservas value */
  brandAccent: '#f2a431',
  /* TODO(brand): replace with official Banreservas value */
  brandAccentContrast: '#14233d',
  /* TODO(brand): replace with official Banreservas value */
  brandAction: '#1ba7ea',
  /* TODO(brand): replace with official Banreservas value */
  brandActionHover: '#41b6ef',
  /* TODO(brand): replace with official Banreservas value */
  brandActionPress: '#1196d7',
  /* TODO(brand): replace with official Banreservas value */
  brandActionContrast: '#0e2747',
  /* TODO(brand): replace with official Banreservas value */
  ink: '#14233d',
  /* TODO(brand): replace with official Banreservas value */
  inkMuted: '#4f5d76',
  /* TODO(brand): replace with official Banreservas value */
  surface: '#ffffff',
  /* TODO(brand): replace with official Banreservas value */
  surfaceSunken: '#f1f4f9',
  /* TODO(brand): replace with official Banreservas value */
  hairline: '#dde3ec',
  /* TODO(brand): replace with official Banreservas value */
  controlLine: '#7f8ca3',
  /* TODO(brand): replace with official Banreservas value */
  danger: '#b3261e',
  /* TODO(brand): replace with official Banreservas value */
  dangerTint: '#fbeae9',
  /* TODO(brand): replace with official Banreservas value */
  disabledBg: '#e3e7ee',
  /* TODO(brand): replace with official Banreservas value */
  disabledInk: '#5f6c83',
  radiusControl: '12px',
  radiusPanel: '16px',
  radiusPlate: '10px',
  motionDuration: '180ms',
  motionEase: 'cubic-bezier(.22, 1, .36, 1)',
} as const;
