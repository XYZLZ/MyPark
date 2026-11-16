/** How many spaces the Inicio and Welcome preview draws (M2B-49). */
export const PREVIEW_COUNT = 40;
/** Columns of the grid, on the preview and on the full map (PF6). */
export const MAP_COLUMNS = 10;
/** Gap between cells, in dp. */
export const MAP_GAP = 4;
/** The card's padding plus its 1 dp border, on each side. */
export const MAP_CARD_PADDING = 16;
export const MAP_CARD_BORDER = 1;
/** Style of every card that holds the grid. With the `border` class the sides add up to MAP_CARD_PADDING, which `cellSize` assumes. */
export const mapCardStyle = { padding: MAP_CARD_PADDING - MAP_CARD_BORDER } as const;

/** The screen gutter on each side (M2B-52); mirrors SCREEN_GUTTER. */
const GUTTER = 16;

/** The widest whole-dp cell that fits `columns` columns inside the card at this window width (M2B-52, design §9.3). */
export function cellSize(
  windowWidth: number,
  columns = MAP_COLUMNS,
  gap = MAP_GAP,
  cardPadding = MAP_CARD_PADDING,
): number {
  return Math.max(1, Math.floor((windowWidth - 2 * GUTTER - 2 * cardPadding - gap * (columns - 1)) / columns));
}

/** The exact width of a full row of cells, so the grid wraps after the last column and never before. */
export function mapGridWidth(size: number, columns = MAP_COLUMNS, gap = MAP_GAP): number {
  return columns * size + (columns - 1) * gap;
}
