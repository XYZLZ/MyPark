/** Map zoom: 1 to 2 in 0.25 steps (PF6, as in the mockup). */
export const ZOOM_LEVELS = [1, 1.25, 1.5, 1.75, 2] as const;
export type ZoomLevel = (typeof ZOOM_LEVELS)[number];
