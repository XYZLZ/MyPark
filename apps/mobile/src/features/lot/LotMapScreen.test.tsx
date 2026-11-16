import { fireEvent, screen } from '@testing-library/react-native';

import { http } from '@/services/runtime';
import { renderScreen } from '@/testing/render';
import { stubLotApi } from '@/testing/lot-api';

import { LotMapScreen } from './LotMapScreen';

jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));
jest.mock('@/features/lot/live/useLiveLot', () => ({
  useLiveLot: () => ({ status: 'live', lastFrameAt: 0, spacesReady: true }),
}));

const hidden = { includeHiddenElements: true } as const;

beforeEach(() => {
  jest.mocked(http.request).mockReset();
  stubLotApi(jest.mocked(http.request));
});

test('the full map shows 280 cells in code order', async () => {
  await renderScreen(<LotMapScreen edge="tabBar" />, { language: 'es-DO' });
  await screen.findByTestId('space-E280', hidden);
  const codes = screen
    .getAllByTestId(/^space-E\d{3}$/, hidden)
    .map((node) => String(node.props.testID).slice('space-'.length));
  expect(codes).toHaveLength(280);
  expect(codes).toEqual([...codes].sort());
  expect(codes[0]).toBe('E001');
});

test('it shows the status and the freshness line but no count (PF19)', async () => {
  await renderScreen(<LotMapScreen edge="tabBar" />, { language: 'es-DO' });
  expect(await screen.findByText('Abierto')).toBeOnTheScreen();
  expect(screen.getByText(/En tiempo real/)).toBeOnTheScreen();
  expect(screen.queryByText('211')).toBeNull();
});

test('the legend names the three shapes', async () => {
  await renderScreen(<LotMapScreen edge="tabBar" />, { language: 'es-DO' });
  for (const label of ['Disponible', 'Ocupado', 'Fuera de servicio']) expect(await screen.findByText(label)).toBeOnTheScreen();
});

test('List free spaces expands a text list', async () => {
  await renderScreen(<LotMapScreen edge="tabBar" />, { language: 'es-DO' });
  const toggle = await screen.findByRole('button', { name: 'Ver espacios libres' });
  expect(toggle.props.accessibilityState).toMatchObject({ expanded: false });
  expect(screen.queryByText(/E067/)).toBeNull();

  await fireEvent.press(toggle);

  expect(screen.getByText(/E067/)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Ver espacios libres' }).props.accessibilityState).toMatchObject({ expanded: true });
});
