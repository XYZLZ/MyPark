import { errorBody, listBody, vehicleFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen } from '@testing-library/react-native';

import { http } from '@/services/runtime';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { VehiclesScreen } from './VehiclesScreen';

const mockRouter = { push: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);

const second = { ...vehicleFixture, id: 'b1b2b3b4-0000-4000-8000-000000000002', plate: 'B987654', make: 'Honda', model: 'Fit', color: 'Blue' };

beforeEach(() => {
  mockRouter.push.mockReset();
  request.mockReset();
});

test('loading shows a skeleton', async () => {
  request.mockReturnValue(new Promise(() => undefined));
  await renderScreen(<VehiclesScreen />);
  expect(screen.getByLabelText('Updating your vehicles')).toBeTruthy();
});

test('empty shows the ghost plate and add-your-first-car', async () => {
  request.mockResolvedValue(reply(200, listBody([], 5)));
  await renderScreen(<VehiclesScreen />);

  expect(await screen.findByText('Add your first car')).toBeTruthy();
  await fireEvent.press(screen.getByRole('button', { name: 'Add vehicle' }));
  expect(mockRouter.push).toHaveBeenCalledWith('/vehicles/new');
});

test('lists plates as text and the count', async () => {
  request.mockResolvedValue(reply(200, listBody([vehicleFixture, second], 5)));
  await renderScreen(<VehiclesScreen />, { language: 'es-DO' });

  expect(await screen.findByText('A123456')).toBeTruthy();
  expect(screen.getByText('B987654')).toBeTruthy();
  expect(screen.getByText('Kia Rio · Red')).toBeTruthy();
  expect(screen.getByText('2 de 5')).toBeTruthy();
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ method: 'GET', path: '/v1/vehicles', auth: true }));

  await fireEvent.press(screen.getByRole('button', { name: 'Editar A123456' }));
  expect(mockRouter.push).toHaveBeenCalledWith(`/vehicles/${vehicleFixture.id}`);
});

test('at cap disables Add with a visible reason', async () => {
  const full = Array.from({ length: 5 }, (_, i) => ({ ...vehicleFixture, id: `00000000-0000-4000-8000-00000000000${i}`, plate: `AA${i}00` }));
  request.mockResolvedValue(reply(200, listBody(full, 5)));
  await renderScreen(<VehiclesScreen />);

  expect(await screen.findByText('You reached the limit of 5 vehicles. Delete one to add another.')).toBeTruthy();
  const add = screen.getByRole('button', { name: 'Add vehicle' });
  expect(add.props.accessibilityState).toMatchObject({ disabled: true });
  await fireEvent.press(add);
  expect(mockRouter.push).not.toHaveBeenCalled();
});

test('error shows retry', async () => {
  request.mockResolvedValueOnce(reply(500, errorBody('internal.unexpected')));
  await renderScreen(<VehiclesScreen />);

  expect(await screen.findByText('We could not load your vehicles')).toBeTruthy();
  request.mockResolvedValueOnce(reply(200, listBody([vehicleFixture], 5)));
  await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('A123456')).toBeTruthy();
});
