import { errorBody, successBody, vehicleFixture } from '@mypark/contracts/fixtures';
import { QueryClient } from '@tanstack/react-query';
import { act, fireEvent, screen, waitFor } from '@testing-library/react-native';
import { Alert, type AlertButton } from 'react-native';

import { http } from '@/services/runtime';
import { renderScreen } from '@/testing/render';
import { reply } from '@/testing/response';

import { VehicleFormScreen } from './VehicleFormScreen';

const mockRouter = { push: jest.fn(), dismissTo: jest.fn() };
jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('@/services/runtime', () => ({ http: { request: jest.fn() } }));

const request = jest.mocked(http.request);
const ID = vehicleFixture.id;
const NEW_BODY = { plate: 'A 123-456', make: 'Kia', model: 'Rio', color: 'Red' };

let alert: jest.SpyInstance;

beforeEach(() => {
  mockRouter.push.mockReset();
  mockRouter.dismissTo.mockReset();
  request.mockReset();
  alert = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
});

afterEach(() => jest.restoreAllMocks());

async function fillIn() {
  await fireEvent.changeText(screen.getByLabelText('Plate'), NEW_BODY.plate);
  await fireEvent.changeText(screen.getByLabelText('Make'), NEW_BODY.make);
  await fireEvent.changeText(screen.getByLabelText('Model'), NEW_BODY.model);
  await fireEvent.changeText(screen.getByLabelText('Color'), NEW_BODY.color);
}

test('create posts what the user typed', async () => {
  request.mockResolvedValue(reply(201, successBody(vehicleFixture)));
  await renderScreen(<VehicleFormScreen />);

  await fillIn();
  await fireEvent.press(screen.getByRole('button', { name: 'Save vehicle' }));

  await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
  expect(request).toHaveBeenCalledTimes(1);
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ method: 'POST', path: '/v1/vehicles', body: NEW_BODY, auth: true }));
});

test('double tap on save sends one request', async () => {
  let release: (value: ReturnType<typeof reply>) => void = () => undefined;
  request.mockReturnValue(new Promise((resolve) => (release = resolve)));
  await renderScreen(<VehicleFormScreen />);

  await fillIn();
  const save = screen.getByRole('button', { name: 'Save vehicle' });
  await fireEvent.press(save);
  await fireEvent.press(save);

  await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
  release(reply(201, successBody(vehicleFixture)));
  await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledTimes(1));
  expect(request).toHaveBeenCalledTimes(1);
});

test.each([
  ['en', 'Plate', 'Save vehicle', 'That plate is already registered.'],
  ['es-DO', 'Placa', 'Guardar vehículo', 'Esa placa ya está registrada.'],
] as const)('plate_taken lands under plate in %s', async (language, plateLabel, save, message) => {
  request.mockResolvedValue(reply(409, errorBody('vehicle.plate_taken')));
  await renderScreen(<VehicleFormScreen />, { language });

  await fireEvent.changeText(screen.getByLabelText(plateLabel), NEW_BODY.plate);
  await fireEvent.changeText(screen.getByLabelText(language === 'en' ? 'Make' : 'Marca'), NEW_BODY.make);
  await fireEvent.changeText(screen.getByLabelText(language === 'en' ? 'Model' : 'Modelo'), NEW_BODY.model);
  await fireEvent.changeText(screen.getByLabelText('Color'), NEW_BODY.color);
  await fireEvent.press(screen.getByRole('button', { name: save }));

  expect(await screen.findByLabelText(`${plateLabel}, ${message}`)).toBeTruthy();
  // It belongs to the field, so there is no banner.
  expect(screen.queryAllByTestId('banner')).toHaveLength(0);
  expect(mockRouter.dismissTo).not.toHaveBeenCalled();
});

test('limit_reached shows a banner', async () => {
  request.mockResolvedValue(reply(409, errorBody('vehicle.limit_reached', { limit: 5 })));
  await renderScreen(<VehicleFormScreen />);

  await fillIn();
  await fireEvent.press(screen.getByRole('button', { name: 'Save vehicle' }));

  expect(await screen.findByText('You reached the limit of 5 vehicles.')).toBeTruthy();
  expect(screen.getAllByTestId('banner')).toHaveLength(1);
});

test('a 429 locks Save for the wait time', async () => {
  request.mockResolvedValue(reply(429, errorBody('rate_limit.exceeded', { retry_after_seconds: 30 })));
  await renderScreen(<VehicleFormScreen />);

  await fillIn();
  await fireEvent.press(screen.getByRole('button', { name: 'Save vehicle' }));

  expect(await screen.findByText('Too many attempts. Try again in 30 s.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Save vehicle' }).props.accessibilityState).toMatchObject({ disabled: true });
});

describe('edit', () => {
  test('edit loads the vehicle and patches only what changed', async () => {
    request.mockResolvedValueOnce(reply(200, successBody(vehicleFixture)));
    await renderScreen(<VehicleFormScreen vehicleId={ID} />);

    await waitFor(() => expect(screen.getByLabelText('Plate').props.value).toBe('A123456'));
    expect(request).toHaveBeenCalledWith(expect.objectContaining({ method: 'GET', path: `/v1/vehicles/${ID}`, auth: true }));
    expect(screen.getByLabelText('Color').props.value).toBe('Red');

    request.mockResolvedValueOnce(reply(200, successBody({ ...vehicleFixture, color: 'Blue' })));
    await fireEvent.changeText(screen.getByLabelText('Color'), 'Blue');
    await fireEvent.press(screen.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
    expect(request).toHaveBeenLastCalledWith(
      expect.objectContaining({ method: 'PATCH', path: `/v1/vehicles/${ID}`, body: { color: 'Blue' }, auth: true }),
    );
  });

  test('saving with nothing changed sends no request', async () => {
    request.mockResolvedValueOnce(reply(200, successBody(vehicleFixture)));
    await renderScreen(<VehicleFormScreen vehicleId={ID} />);
    await waitFor(() => expect(screen.getByLabelText('Plate').props.value).toBe('A123456'));

    await fireEvent.press(screen.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
    expect(request).toHaveBeenCalledTimes(1);
  });

  async function openDelete() {
    request.mockResolvedValueOnce(reply(200, successBody(vehicleFixture)));
    await renderScreen(<VehicleFormScreen vehicleId={ID} />);
    await waitFor(() => expect(screen.getByLabelText('Plate').props.value).toBe('A123456'));
    await fireEvent.press(screen.getByRole('button', { name: 'Delete vehicle' }));
    const buttons = alert.mock.calls[0]![2] as AlertButton[];
    return buttons.find((button) => button.style === 'destructive')!;
  }

  test('delete confirms with a native Alert before calling the API', async () => {
    const destructive = await openDelete();

    expect(alert).toHaveBeenCalledWith('Delete vehicle A123456?', undefined, expect.any(Array));
    expect(destructive.text).toBe('Yes, delete');
    expect(request.mock.calls.some(([req]) => req.method === 'DELETE')).toBe(false);

    request.mockResolvedValueOnce(reply(204));
    await act(async () => destructive.onPress?.());

    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ method: 'DELETE', path: `/v1/vehicles/${ID}`, auth: true }));
  });

  test('while the DELETE is in flight Delete shows loading and Save and Delete are disabled', async () => {
    const destructive = await openDelete();

    let finish: (value: ReturnType<typeof reply>) => void = () => undefined;
    request.mockReturnValueOnce(new Promise((resolve) => (finish = resolve)));
    await act(async () => destructive.onPress?.());

    await waitFor(() => expect(screen.getByRole('button', { name: 'Delete vehicle' }).props.accessibilityState).toMatchObject({ busy: true, disabled: true }));
    expect(screen.getByRole('button', { name: 'Save changes' }).props.accessibilityState).toMatchObject({ disabled: true });

    finish(reply(204));
    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
  });

  test('the edit loading skeleton is announced', async () => {
    request.mockReturnValueOnce(new Promise(() => undefined));
    await renderScreen(<VehicleFormScreen vehicleId={ID} />);
    expect(screen.getByRole('progressbar', { name: 'Loading…' })).toBeTruthy();
  });

  test('vehicle_in_use shows a banner', async () => {
    const destructive = await openDelete();

    request.mockResolvedValueOnce(reply(409, errorBody('vehicle.in_use')));
    await act(async () => destructive.onPress?.());

    expect(await screen.findByText('This vehicle is in use and cannot be deleted.')).toBeTruthy();
    expect(mockRouter.dismissTo).not.toHaveBeenCalled();
  });

  test('not_found returns to the list and invalidates', async () => {
    request.mockResolvedValueOnce(reply(404, errorBody('vehicle.not_found')));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
    const invalidate = jest.spyOn(client, 'invalidateQueries');
    await renderScreen(<VehicleFormScreen vehicleId={ID} />, { queryClient: client });

    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
    expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ['vehicles'] }));
  });

  test('not_found while deleting also returns to the list', async () => {
    const destructive = await openDelete();

    request.mockResolvedValueOnce(reply(404, errorBody('vehicle.not_found')));
    await act(async () => destructive.onPress?.());

    await waitFor(() => expect(mockRouter.dismissTo).toHaveBeenCalledWith('/vehicles'));
  });
});
