import { spacesFixture } from '@mypark/contracts/fixtures';
import { fireEvent, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, test } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { FreeSpacesList } from './FreeSpacesList';
import { MapLegend } from './MapLegend';
import { SpaceMap, ZOOM_LEVELS, type ZoomLevel } from './SpaceMap';
import { SpacesSummary } from './SpacesSummary';
import { ZoomControls } from './ZoomControls';

const spaces = spacesFixture.spaces;
const renderMap = (props: Partial<Parameters<typeof SpaceMap>[0]> = {}) =>
  renderWithProviders(<SpaceMap spaces={spaces} ready zoom={1} {...props} />, { locale: 'es-DO' });
const cell = (container: HTMLElement, code: string) => container.querySelector<HTMLElement>(`[data-code="${code}"]`)!;

describe('SpaceMap', () => {
  test('the grid is hidden from assistive tech and the summary reads the counts', () => {
    const { container } = renderMap();
    expect(container.querySelector('[data-code="E001"]')!.closest('[aria-hidden="true"]')).not.toBeNull();
    expect(container.querySelectorAll('[data-code]')).toHaveLength(280);
    expect(container.querySelectorAll('[data-code][tabindex], button [data-code]')).toHaveLength(0);

    renderWithProviders(<SpacesSummary spaces={spaces} />, { locale: 'es-DO' });
    expect(screen.getByText('Sensores — libres: 211, ocupados: 66, fuera de servicio: 3.')).toBeInTheDocument();
  });

  test('each state has its own shape, not only color', () => {
    const { container } = renderMap();
    expect(cell(container, 'E067').dataset.state).toBe('free');
    expect(cell(container, 'E067').querySelector('[data-shape]')).toBeNull();
    expect(cell(container, 'E001').dataset.state).toBe('occupied');
    expect(cell(container, 'E001').querySelector('[data-shape="car"]')).not.toBeNull();
    expect(cell(container, 'E278').dataset.state).toBe('out_of_service');
    expect(cell(container, 'E278')).toHaveTextContent('×');
  });

  test('the legend repeats each shape with text', () => {
    const { container } = renderWithProviders(<MapLegend />, { locale: 'es-DO' });
    for (const label of ['Disponible', 'Ocupado', 'Fuera de servicio']) expect(screen.getByText(label)).toBeInTheDocument();
    expect(container.querySelector('[data-shape="car"]')).not.toBeNull();
    expect(container).toHaveTextContent('×');
  });

  test('the free list gives codes as text', () => {
    renderWithProviders(<FreeSpacesList spaces={spaces} />, { locale: 'es-DO' });
    const summary = screen.getByText('Ver espacios libres');
    expect(summary.tagName).toBe('SUMMARY');
    const details = summary.closest('details')!;
    expect(details).toHaveTextContent('E067');
    expect(details).not.toHaveTextContent('E001');
  });

  test('zoom buttons are labelled and stop at the bounds', () => {
    function Harness() {
      const [zoom, setZoom] = useState<ZoomLevel>(1);
      return <ZoomControls zoom={zoom} onZoom={setZoom} />;
    }
    renderWithProviders(<Harness />, { locale: 'es-DO' });
    const out = screen.getByRole('button', { name: 'Alejar mapa' });
    const zoomIn = screen.getByRole('button', { name: 'Acercar mapa' });
    expect(ZOOM_LEVELS).toEqual([1, 1.25, 1.5, 1.75, 2]);
    expect(out).toBeDisabled();
    for (let i = 0; i < 4; i++) fireEvent.click(zoomIn);
    expect(zoomIn).toBeDisabled();
    expect(out).toBeEnabled();
  });

  test('zoom widens the map inside its own scroll frame', () => {
    const { container } = renderMap({ zoom: 1.5 });
    const frame = container.querySelector<HTMLElement>('.overflow-auto')!;
    expect((frame.firstElementChild as HTMLElement).style.width).toBe('150%');
  });

  test('no spaces yet shows the skeleton; not ready shows Actualizando el mapa… at 60 %', () => {
    const first = renderMap({ spaces: null });
    expect(first.container.querySelectorAll('[data-code]')).toHaveLength(0);
    expect(first.container.querySelector('[data-skeleton]')).not.toBeNull();
    expect(screen.queryByText('Actualizando el mapa…')).toBeNull();
    first.unmount();

    const second = renderMap({ ready: false });
    expect(screen.getByText('Actualizando el mapa…')).toBeInTheDocument();
    expect(second.container.querySelector('.opacity-60')).not.toBeNull();
  });

  test('the note says the map shows sensor readings', () => {
    renderWithProviders(<SpacesSummary spaces={spaces} />, { locale: 'es-DO' });
    expect(screen.getByText('El mapa muestra lo que leen los sensores.')).toBeInTheDocument();
  });
});

