import { availabilityReducedFixture, lotFixture, tariffFixture } from '@mypark/contracts/fixtures';
import { availabilityFigures } from '@mypark/contracts';
import { act, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { renderWithProviders } from '@/testing/render';
import { Announcer } from './Announcer';
import { CountCard } from './CountCard';
import { FreshnessLine } from './FreshnessLine';
import { HomeHero } from './HomeHero';
import { LotErrorPanel } from './LotErrorPanel';
import { LotSkeleton } from './LotSkeleton';
import { LotInfo } from './LotInfo';
import { RatesList } from './RatesList';
import { StatusPill } from './StatusPill';
import { router } from '@/testing/navigation';

const NOW = Date.parse('2026-11-15T14:50:00Z');
const figures = availabilityFigures(availabilityReducedFixture);

describe('RatesList', () => {
  test('RatesList shows the six lines in RD$', () => {
    renderWithProviders(<RatesList tariff={{ ...tariffFixture, reservation_fee_cents: 12345 }} />, { locale: 'es-DO' });

    const rows = screen.getAllByRole('listitem').map((row) => row.textContent);
    expect(rows).toEqual([
      'Primera horaRD$50.00',
      'Hora adicionalRD$25.00',
      'Máximo por díaRD$400.00',
      'Salida sin costo10 min',
      'Salir tras pagar15 min',
      'Ticket perdidoRD$500.00',
    ]);
    expect(screen.getByRole('heading', { level: 2, name: 'Tarifas' })).toBeInTheDocument();
    expect(screen.queryByText(/123\.45/)).not.toBeInTheDocument();
  });

  test('RatesList prints minutes in English too', () => {
    renderWithProviders(<RatesList tariff={tariffFixture} />, { locale: 'en' });

    expect(screen.getByText('RD$50.00')).toBeInTheDocument();
    expect(screen.getByText('10 min')).toBeInTheDocument();
    expect(screen.getByText('15 min')).toBeInTheDocument();
  });
});

describe('LotInfo', () => {
  test('LotInfo opens Maps in a new tab and says so', () => {
    renderWithProviders(<LotInfo lot={lotFixture} />, { locale: 'es-DO' });

    const link = screen.getByRole('link', { name: /Abrir en Maps/ });
    expect(link).toHaveAttribute('href', lotFixture.map_url);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByText('(se abre en una pestaña nueva)')).toBeInTheDocument();
    expect(screen.getByText(lotFixture.address)).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Información del parqueo' })).toBeInTheDocument();
  });

  test('LotInfo hours: always open vs varies', () => {
    const { rerender } = renderWithProviders(<LotInfo lot={lotFixture} />, { locale: 'en' });
    expect(screen.getByText('Open 24 hours')).toBeInTheDocument();

    rerender(<LotInfo lot={{ ...lotFixture, opening_hours: { always_open: false } }} />);
    expect(screen.getByText('Hours vary')).toBeInTheDocument();
    expect(screen.queryByText('Open 24 hours')).not.toBeInTheDocument();
  });

  test.each(['javascript:alert(1)', 'data:text/html,x', '', 'not a url'])('LotInfo never links a non-http map_url (%s)', (map_url) => {
    renderWithProviders(<LotInfo lot={{ ...lotFixture, map_url }} />, { locale: 'en' });

    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText(lotFixture.address)).toBeInTheDocument();
  });
});

describe('StatusPill', () => {
  test.each([
    ['open', 'Abierto'],
    ['full', 'Lleno'],
    ['closed', 'Cerrado'],
  ] as const)('StatusPill carries text, not only color (%s)', (status, text) => {
    renderWithProviders(<StatusPill status={status} />, { locale: 'es-DO' });

    expect(screen.getByText(text)).toBeInTheDocument();
  });
});

describe('CountCard', () => {
  test('shows the count, the capacity and the out of service line', () => {
    renderWithProviders(<CountCard figures={figures} status="open" />, { locale: 'es-DO' });

    expect(screen.getByText('211')).toBeInTheDocument();
    expect(screen.getByText('de 277 espacios · 3 fuera de servicio')).toBeInTheDocument();
  });

  test('a closed lot replaces the number with Cerrado and says why', () => {
    renderWithProviders(<CountCard figures={figures} status="closed" />, { locale: 'es-DO' });

    expect(screen.queryByText('211')).not.toBeInTheDocument();
    expect(screen.getAllByText('Cerrado').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('El parqueo no recibe vehículos ahora.')).toBeInTheDocument();
  });
});

describe('FreshnessLine', () => {
  const text = () => screen.getByTestId('freshness').textContent!.replace(/[\s  ]+/g, ' ');
  const props = { updatedAt: '2026-11-15T14:47:00Z', lastFrameAt: null, snapshotAt: NOW } as const;

  function renderLine(overrides: Partial<Parameters<typeof FreshnessLine>[0]> & { connection: Parameters<typeof FreshnessLine>[0]['connection'] }) {
    return renderWithProviders(
      <div data-testid="freshness">
        <FreshnessLine {...props} {...overrides} />
      </div>,
      { locale: 'es-DO' },
    );
  }

  test('live says En tiempo real and the last change', () => {
    renderLine({ connection: 'live' });
    expect(text()).toBe('En tiempo real · Último cambio 10:47 a. m.');
  });

  test('an invalid time leaves no dangling label or space', () => {
    renderLine({ connection: 'live', updatedAt: 'not a time' });
    expect(text()).toBe('En tiempo real');
    expect(screen.getByTestId('freshness').textContent).not.toMatch(/\s$/);
  });

  test('stale uses the last frame, not the last change, and says it is reconnecting', () => {
    renderLine({ connection: 'stale', lastFrameAt: Date.parse('2026-11-15T14:49:00Z') });
    expect(text()).toContain('Desactualizado — última actualización 10:49 a. m.');
    expect(text()).toContain('Reconectando…');
  });

  test('stale with no frame falls back to the snapshot time', () => {
    renderLine({ connection: 'stale' });
    expect(text()).toContain('Desactualizado — última actualización 10:50 a. m.');
  });

  test('stale and unavailable survive an invalid time without a dangling space', () => {
    const { unmount } = renderLine({ connection: 'stale', snapshotAt: Number.NaN });
    expect(text()).toContain('Desactualizado — última actualización');
    expect(screen.getByTestId('freshness').textContent).not.toMatch(/ —\s*$|\s{2}/);
    unmount();
    renderLine({ connection: 'unavailable', snapshotAt: Number.NaN });
    expect(text()).toBe('Actualizaciones en vivo no disponibles');
  });

  test('connecting and unavailable', () => {
    const { unmount } = renderLine({ connection: 'connecting' });
    expect(text()).toBe('Conectando…');
    unmount();
    renderLine({ connection: 'unavailable' });
    expect(text()).toBe('Actualizaciones en vivo no disponibles · datos de las 10:50 a. m.');
  });
});

describe('placement and the hero placeholder', () => {
  test('the error panel and the skeleton leave grid placement to PublicHome', () => {
    const panel = renderWithProviders(<LotErrorPanel />, { locale: 'en' });
    expect(panel.container.innerHTML).not.toMatch(/lg:(col|row)-start/);
    panel.unmount();
    const skeleton = renderWithProviders(<LotSkeleton />, { locale: 'en' });
    expect(skeleton.container.innerHTML).not.toMatch(/lg:(col|row)-start/);
  });

  test('the hero photo placeholder is hidden from assistive technology', () => {
    const { container } = renderWithProviders(<HomeHero />, { locale: 'es-DO' });
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
    expect(container.querySelector('[aria-hidden="true"][data-photo-placeholder]')).not.toBeNull();
  });
});

describe('LotErrorPanel', () => {
  test('retry refreshes the route by default and calls onRetry when given', async () => {
    const { unmount } = renderWithProviders(<LotErrorPanel />, { locale: 'es-DO' });
    expect(screen.getByRole('heading', { level: 2, name: 'La disponibilidad no está cargando ahora' })).toBeInTheDocument();
    act(() => screen.getByRole('button', { name: 'Reintentar' }).click());
    expect(router.refresh).toHaveBeenCalledTimes(1);
    unmount();

    const onRetry = vi.fn();
    renderWithProviders(<LotErrorPanel onRetry={onRetry} />, { locale: 'en' });
    act(() => screen.getByRole('button', { name: 'Try again' }).click());
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(router.refresh).toHaveBeenCalledTimes(1);
  });
});

describe('Announcer', () => {
  beforeEach(() => {
    vi.useFakeTimers({ now: NOW });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const ui = (over: Partial<Parameters<typeof Announcer>[0]> = {}) => (
    <Announcer figures={figures} status="open" connection="live" {...over} />
  );
  const withCount = (available: number) => ({ ...figures, available });
  const region = () => screen.getByRole('status');

  test('is silent on mount', () => {
    renderWithProviders(ui(), { locale: 'es-DO' });
    expect(region()).toBeEmptyDOMElement();
  });

  test.each([
    ['full', 'El parqueo está lleno.'],
    ['closed', 'El parqueo está cerrado.'],
  ] as const)('says a change to %s at once', (status, sentence) => {
    const { rerender } = renderWithProviders(ui(), { locale: 'es-DO' });
    rerender(ui({ status }));
    expect(region()).toHaveTextContent(sentence);
  });

  test('says a change back to open with announce.open', () => {
    const { rerender } = renderWithProviders(ui({ status: 'full' }), { locale: 'es-DO' });
    rerender(ui({ status: 'open' }));
    expect(region()).toHaveTextContent('El parqueo está abierto.');
  });

  test('a count change deferred inside the 30 s window is announced by the due time (Task 7 carry)', () => {
    const { rerender } = renderWithProviders(ui(), { locale: 'es-DO' });
    act(() => vi.advanceTimersByTime(2_000));
    rerender(ui({ figures: withCount(210) }));
    expect(region()).toBeEmptyDOMElement();

    act(() => vi.advanceTimersByTime(27_999));
    expect(region()).toBeEmptyDOMElement();
    act(() => vi.advanceTimersByTime(1));
    expect(region()).toHaveTextContent('Espacios disponibles: 210 de 277.');
  });

  test('a status change after a due count is not overwritten by the count a few ms later', () => {
    const { rerender } = renderWithProviders(ui(), { locale: 'es-DO' });
    act(() => vi.advanceTimersByTime(1_000));
    rerender(ui({ figures: withCount(210) }));
    // A throttled tab: the clock passed the due time but the timer has not run yet.
    act(() => vi.setSystemTime(NOW + 40_000));
    rerender(ui({ figures: withCount(210), status: 'full' }));
    expect(region()).toHaveTextContent('El parqueo está lleno.');

    act(() => vi.advanceTimersByTime(50));
    expect(region()).toHaveTextContent('El parqueo está lleno.');
    expect(region()).not.toHaveTextContent('Espacios disponibles');

    act(() => vi.advanceTimersByTime(5_000));
    expect(region()).toHaveTextContent('Espacios disponibles: 210 de 277.');
  });

  test('a stale connection says when the data was last updated', () => {
    const { rerender } = renderWithProviders(ui(), { locale: 'es-DO' });
    rerender(ui({ connection: 'stale', lastUpdateAt: Date.parse('2026-11-15T14:52:00Z') }));
    expect(region()).toHaveTextContent('Los datos están desactualizados. Última actualización 10:52 a. m.');
  });

  test('speaks English with the English sentences', () => {
    const { rerender } = renderWithProviders(ui(), { locale: 'en' });
    rerender(ui({ status: 'full' }));
    expect(region()).toHaveTextContent('The parking lot is full.');
  });
});
