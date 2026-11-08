import type { Vehicle } from '@mypark/contracts';
import { errorBody, listBody, vehicleFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import en from '@/i18n/messages/en.json';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { renderWithProviders } from '@/testing/render';
import { VehiclesScreen } from './VehiclesScreen';

const V = en.vehicles;

const car = (n: number, plate = `A12345${n}`): Vehicle => ({
  ...vehicleFixture,
  id: `7d8e9f0a-1b2c-4d3e-8f4a-5b6c7d8e9f0${n}`,
  plate,
  plate_normalized: plate.replace(/[ -]/g, '').toUpperCase(),
  make: n === 1 ? 'Kia' : 'Honda',
  model: n === 1 ? 'Rio' : 'CR-V',
  color: n === 1 ? 'Red' : 'Blue',
});

function renderScreen(items: Vehicle[], { limit = 5, notice }: { limit?: number; notice?: 'vehicle_not_found' } = {}) {
  return renderWithProviders(
    <VehiclesScreen initial={{ items, page: { limit, next_cursor: null } }} initialUpdatedAt={Date.now()} notice={notice} />,
    { locale: 'en' },
  );
}

/** Answers by method so a test can script the DELETE and the GETs that follow. */
function scripted(handlers: { delete?: () => Response; get?: () => Response }) {
  return stubFetch(async (_url, init) =>
    init.method === 'DELETE'
      ? (handlers.delete?.() ?? jsonResponse(204))
      : (handlers.get?.() ?? jsonResponse(200, listBody([], 5))),
  );
}

const calls = (mock: ReturnType<typeof stubFetch>, method: string) =>
  mock.mock.calls.filter(([, init]) => (init?.method ?? 'GET') === method);

function confirmDelete(plate: string) {
  fireEvent.click(screen.getByRole('button', { name: `Delete ${plate}` }));
  fireEvent.click(screen.getByRole('button', { name: V.delete.confirmAction }));
}

describe('VehiclesScreen', () => {
  test('empty state shows the ghost plate and add-your-first-car', () => {
    renderScreen([]);

    expect(screen.getByRole('heading', { name: V.empty.title })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: V.add })).toHaveAttribute('href', '/en/vehicles/new');
    expect(screen.queryByRole('list')).not.toBeInTheDocument();
  });

  test('lists plate, make and model, color, Edit and Delete', () => {
    renderScreen([car(1, 'a-1 23')]);

    const row = screen.getByRole('listitem');
    expect(within(row).getByText('a-1 23')).toBeInTheDocument(); // as the driver typed it
    expect(within(row).getByText('Kia Rio')).toBeInTheDocument();
    expect(within(row).getByText('Red')).toBeInTheDocument();
    expect(within(row).getByRole('link', { name: 'Edit a-1 23' })).toHaveAttribute('href', `/en/vehicles/${car(1).id}/edit`);
    expect(within(row).getByRole('button', { name: 'Delete a-1 23' })).toBeInTheDocument();
  });

  test('shows the count from meta.page.limit', () => {
    renderScreen([car(1), car(2)], { limit: 5 });
    expect(screen.getByText('2 of 5')).toBeInTheDocument();
  });

  test('the cap follows meta.page.limit, not a constant', () => {
    renderScreen([car(1), car(2)], { limit: 2 });
    expect(screen.getByRole('button', { name: V.add })).toBeDisabled();
  });

  test('at cap, Add is disabled with a visible reason', () => {
    renderScreen([1, 2, 3, 4, 5].map((n) => car(n)), { limit: 5 });

    const add = screen.getByRole('button', { name: V.add });
    expect(add).toBeDisabled();
    const reason = screen.getByText('You reached the limit of 5 vehicles. Delete one to add another.');
    expect(reason).toBeVisible();
    expect(add).toHaveAccessibleDescription(reason.textContent!);
    expect(screen.queryByRole('link', { name: V.add })).not.toBeInTheDocument();
  });

  test('delete expands an inline confirmation in the row', () => {
    renderScreen([car(1), car(2)]);

    fireEvent.click(screen.getByRole('button', { name: 'Delete A123451' }));

    const row = screen.getAllByRole('listitem')[0]!;
    expect(within(row).getByText('Delete vehicle A123451?')).toBeInTheDocument();
    expect(within(row).getByRole('button', { name: V.delete.confirmAction })).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: V.delete.confirmAction })).toHaveLength(1);

    fireEvent.click(within(row).getByRole('button', { name: V.delete.cancel }));
    expect(screen.queryByText('Delete vehicle A123451?')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete A123451' })).toHaveFocus();
  });

  test('confirm deletes and refetches', async () => {
    const fetchMock = scripted({ get: () => jsonResponse(200, listBody([car(2)], 5)) });
    renderScreen([car(1), car(2)]);

    confirmDelete('A123451');

    await waitFor(() => expect(screen.queryByText('A123451')).not.toBeInTheDocument());
    expect(screen.getByText('A123452')).toBeInTheDocument();
    expect(fetchMock.mock.calls[0]![0]).toBe(`/api/v1/vehicles/${car(1).id}`);
    expect(fetchMock.mock.calls[0]![1]!.method).toBe('DELETE');
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/vehicles');
    expect(fetchMock.mock.calls[1]![1]!.method ?? 'GET').toBe('GET');
  });

  test('double click on confirm sends one DELETE', async () => {
    const fetchMock = scripted({ get: () => jsonResponse(200, listBody([], 5)) });
    renderScreen([car(1)]);

    fireEvent.click(screen.getByRole('button', { name: 'Delete A123451' }));
    const confirm = screen.getByRole('button', { name: V.delete.confirmAction });
    fireEvent.click(confirm);
    fireEvent.click(confirm);

    await waitFor(() => expect(screen.getByRole('heading', { name: V.empty.title })).toBeInTheDocument());
    expect(calls(fetchMock, 'DELETE')).toHaveLength(1);
  });

  test('vehicle.in_use shows a banner', async () => {
    scripted({ delete: () => jsonResponse(409, errorBody('vehicle.in_use')) });
    renderScreen([car(1)]);

    confirmDelete('A123451');

    expect(await screen.findByRole('alert')).toHaveTextContent('This vehicle is in use and cannot be deleted.');
    expect(screen.getByText('A123451')).toBeInTheDocument();
  });

  test('vehicle.not_found invalidates and shows a banner', async () => {
    const fetchMock = scripted({
      delete: () => jsonResponse(404, errorBody('vehicle.not_found')),
      get: () => jsonResponse(200, listBody([], 5)),
    });
    renderScreen([car(1)]);

    confirmDelete('A123451');

    expect(await screen.findByRole('alert')).toHaveTextContent('We could not find that vehicle.');
    await waitFor(() => expect(calls(fetchMock, 'GET')).toHaveLength(1));
    await waitFor(() => expect(screen.queryByText('A123451')).not.toBeInTheDocument());
  });

  test('refetch failure shows a retry panel', async () => {
    let list = jsonResponse(400, errorBody('http.invalid_json'));
    const fetchMock = scripted({ get: () => list });
    renderScreen([car(1)]);

    confirmDelete('A123451');

    expect(await screen.findByText(V.loadError.title)).toBeInTheDocument();
    list = jsonResponse(200, listBody([car(2)], 5));
    fireEvent.click(screen.getByRole('button', { name: V.loadError.retry }));

    expect(await screen.findByText('A123452')).toBeInTheDocument();
    expect(calls(fetchMock, 'GET')).toHaveLength(2);
  });

  test('notice vehicle_not_found shows a banner', () => {
    renderScreen([car(1)], { notice: 'vehicle_not_found' });
    expect(screen.getByRole('alert')).toHaveTextContent('We could not find that vehicle.');
  });
});
