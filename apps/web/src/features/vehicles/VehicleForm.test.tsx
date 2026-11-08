import { errorBody, listBody, successBody, validationBody, vehicleFixture } from '@mypark/contracts/fixtures';
import { useQuery } from '@tanstack/react-query';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import es from '@/i18n/messages/es-DO.json';
import { jsonResponse, sentBody, stubFetch } from '@/testing/fetch';
import { router } from '@/testing/navigation';
import { renderWithProviders } from '@/testing/render';
import { fetchVehicles, VEHICLES_QUERY_KEY } from './api';
import { VehicleForm } from './VehicleForm';

const F = en.vehicles.form;

const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });

function fillCar(plate = 'a-1 23') {
  fill(F.plate, plate);
  fill(F.make, 'Kia');
  fill(F.model, 'Rio');
  fill(F.color, 'Red');
}

const save = () => fireEvent.click(screen.getByRole('button', { name: F.create }));

/** A list query like the one on the vehicles screen, to prove the form invalidates it. */
function ListProbe() {
  useQuery({ queryKey: VEHICLES_QUERY_KEY, queryFn: fetchVehicles });
  return null;
}

describe('VehicleForm', () => {
  test('create posts what the user typed', async () => {
    const fetchMock = stubFetch(jsonResponse(201, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar('a-1 23');

    save();

    await waitFor(() => expect(router.push).toHaveBeenCalledWith('/vehicles'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/vehicles');
    expect(fetchMock.mock.calls[0]![1]!.method).toBe('POST');
    expect(sentBody(fetchMock)).toEqual({ plate: 'a-1 23', make: 'Kia', model: 'Rio', color: 'Red' });
  });

  test.each(['a - 1  23', 'ab-12', 'A123456'])('plate typed as %j is submitted as typed', async (plate) => {
    const fetchMock = stubFetch(jsonResponse(201, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar(plate);

    save();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sentBody(fetchMock).plate).toBe(plate);
  });

  test('a plate made only of separators is rejected before any request', async () => {
    const fetchMock = stubFetch(jsonResponse(201, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar('--');

    save();

    expect(await screen.findByText(en.validation.plate)).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('double click on save sends one request', async () => {
    const fetchMock = stubFetch(jsonResponse(201, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar();

    const button = screen.getByRole('button', { name: F.create });
    fireEvent.click(button);
    fireEvent.click(button);

    await waitFor(() => expect(router.push).toHaveBeenCalled());
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test.each([
    ['en', 'Save vehicle', 'That plate is already registered.', en],
    ['es-DO', 'Guardar vehículo', 'Esa placa ya está registrada.', es],
  ] as const)('plate_taken lands under plate, in %s', async (locale, button, message, messages) => {
    stubFetch(jsonResponse(409, errorBody('vehicle.plate_taken')));
    renderWithProviders(<VehicleForm mode="create" />, { locale });
    const labels = messages.vehicles.form;
    fill(labels.plate, 'A123');
    fill(labels.make, 'Kia');
    fill(labels.model, 'Rio');
    fill(labels.color, 'Red');

    fireEvent.click(screen.getByRole('button', { name: button }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByLabelText(labels.plate)).toBeInvalid();
    expect(screen.getByLabelText(labels.plate)).toHaveAccessibleDescription(expect.stringContaining(message));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  test('server rule plate lands under plate', async () => {
    stubFetch(jsonResponse(422, validationBody([{ field: 'plate', rule: 'plate' }])));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar('AB1');

    save();

    expect(await screen.findByText(en.validation.plate)).toBeInTheDocument();
    expect(screen.getByLabelText(F.plate)).toBeInvalid();
  });

  test('limit_reached shows a banner with the limit', async () => {
    stubFetch(jsonResponse(409, errorBody('vehicle.limit_reached', { limit: 5 })));
    renderWithProviders(<VehicleForm mode="create" />, { locale: 'en' });
    fillCar();

    save();

    expect(await screen.findByRole('alert')).toHaveTextContent('You reached the limit of 5 vehicles.');
  });

  test('edit pre-fills and patches', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody({ ...vehicleFixture, color: 'Blue' })));
    renderWithProviders(<VehicleForm mode="edit" vehicle={vehicleFixture} />, { locale: 'en' });

    expect(screen.getByLabelText(F.plate)).toHaveValue('A123456');
    expect(screen.getByLabelText(F.make)).toHaveValue('Kia');
    fill(F.color, 'Blue');
    fireEvent.click(screen.getByRole('button', { name: F.save }));

    await waitFor(() => expect(router.push).toHaveBeenCalledWith('/vehicles'));
    expect(fetchMock.mock.calls[0]![0]).toBe(`/api/v1/vehicles/${vehicleFixture.id}`);
    expect(fetchMock.mock.calls[0]![1]!.method).toBe('PATCH');
    expect(sentBody(fetchMock)).toEqual({ color: 'Blue' });
  });

  test('edit with no changes sends no request and returns to the list', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="edit" vehicle={vehicleFixture} />, { locale: 'en' });

    fireEvent.click(screen.getByRole('button', { name: F.save }));

    await waitFor(() => expect(router.push).toHaveBeenCalledWith('/vehicles'));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('editing a field back to its original value sends nothing for it', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(vehicleFixture)));
    renderWithProviders(<VehicleForm mode="edit" vehicle={vehicleFixture} />, { locale: 'en' });
    fill(F.make, 'Honda');
    fill(F.make, 'Kia');
    fill(F.model, 'Civic');

    fireEvent.click(screen.getByRole('button', { name: F.save }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sentBody(fetchMock)).toEqual({ model: 'Civic' });
  });

  test('success invalidates vehicles and returns to the list', async () => {
    const fetchMock = stubFetch(async (_url, init) =>
      init.method === 'POST' ? jsonResponse(201, successBody(vehicleFixture)) : jsonResponse(200, listBody([vehicleFixture], 5)),
    );
    renderWithProviders(
      <>
        <ListProbe />
        <VehicleForm mode="create" />
      </>,
      { locale: 'en' },
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1)); // the probe's first GET
    fillCar();

    save();

    await waitFor(() => expect(router.push).toHaveBeenCalledWith('/vehicles'));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([, init]) => (init?.method ?? 'GET') === 'GET')).toHaveLength(2));
  });
});
