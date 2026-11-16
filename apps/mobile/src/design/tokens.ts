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
  /* TODO(brand): replace with official Banreservas value */
  statusOpen: '#1e7a46',
  /* TODO(brand): replace with official Banreservas value */
  statusOpenTint: '#e7f4ec',
  /* TODO(brand): replace with official Banreservas value */
  statusFull: '#b3261e',
  /* TODO(brand): replace with official Banreservas value */
  statusFullTint: '#fbeae9',
  /* TODO(brand): replace with official Banreservas value */
  statusClosed: '#4f5d76',
  /* TODO(brand): replace with official Banreservas value */
  statusClosedTint: '#eceff4',
  /* TODO(brand): replace with official Banreservas value */
  statusStale: '#8a5300',
  /* TODO(brand): replace with official Banreservas value */
  statusStaleTint: '#fdf1dc',
  /* TODO(brand): replace with official Banreservas value */
  brandNavy: '#0b3a75',
  /* TODO(brand): replace with official Banreservas value */
  brandNavyDeep: '#072a57',
  /* TODO(brand): replace with official Banreservas value */
  brandOrange: '#f39a1e',
  /* TODO(brand): replace with official Banreservas value */
  brandSky: '#eef4fb',
  /* TODO(brand): replace with official Banreservas value */
  brandSkyStrong: '#dce9f8',
  /* TODO(brand): replace with official Banreservas value */
  navActive: '#1769c9',
  /* TODO(brand): replace with official Banreservas value */
  onNavyMuted: '#c6d4ea',
  /* TODO(brand): replace with official Banreservas value */
  spaceOccupied: '#c4610a',
  /* TODO(brand): replace with official Banreservas value */
  spaceOccupiedTint: '#fdeedd',
  /* TODO(brand): replace with official Banreservas value */
  lotConcrete: '#e3e8ef',
  /* TODO(brand): replace with official Banreservas value */
  lotLawn: '#d3e9c8',
  /* TODO(brand): replace with official Banreservas value */
  lotTree: '#5c9f62',
  radiusControl: '12px',
  radiusPanel: '16px',
  radiusPlate: '10px',
  motionDuration: '180ms',
  motionEase: 'cubic-bezier(.22, 1, .36, 1)',
} as const;
