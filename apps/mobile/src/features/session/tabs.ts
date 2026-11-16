/** The tab order (M2B-48): Inicio first, so the lot is what a driver sees after login. */
export const TAB_ORDER = ['home', 'vehicles', 'profile'] as const;
export type TabName = (typeof TAB_ORDER)[number];
