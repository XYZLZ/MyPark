import { cellSize, MAP_COLUMNS, MAP_GAP, MAP_CARD_BORDER, MAP_CARD_PADDING, mapCardStyle, mapGridWidth, PREVIEW_COUNT } from './map-layout';

test('the preview is 40 spaces in 10 columns', () => {
  expect(PREVIEW_COUNT).toBe(40);
  expect(MAP_COLUMNS).toBe(10);
});

test.each([
  [360, 26],
  [430, 33],
])('cellSize fits 10 columns inside the card at %i dp', (width, expected) => {
  const size = cellSize(width);
  expect(size).toBe(expected);
  // The card's real inner width: the window minus the gutters, the card padding and the card border on both sides.
  const inner = width - 2 * 16 - 2 * (mapCardStyle.padding + MAP_CARD_BORDER);
  expect(MAP_COLUMNS * size + (MAP_COLUMNS - 1) * MAP_GAP).toBeLessThanOrEqual(inner);
  expect(mapGridWidth(size)).toBe(MAP_COLUMNS * size + (MAP_COLUMNS - 1) * MAP_GAP);
});

test('cellSize never goes below 1 on a degenerate window', () => {
  expect(cellSize(0)).toBe(1);
});

test('the card padding plus its border is the 16 dp the formula assumes', () => {
  expect(mapCardStyle.padding + MAP_CARD_BORDER).toBe(MAP_CARD_PADDING);
});
