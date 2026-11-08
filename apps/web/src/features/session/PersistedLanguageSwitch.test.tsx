import { errorBody } from '@mypark/contracts/fixtures';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { renderWithProviders } from '@/testing/render';
import { PersistedLanguageSwitch } from './PersistedLanguageSwitch';

describe('PersistedLanguageSwitch', () => {
  test.each([
    ['a network failure', () => Promise.reject(new TypeError('offline'))],
    ['a 500', () => jsonResponse(500, errorBody('internal.unexpected'))],
    ['an unparseable body', () => new Response('<html>', { status: 200 })],
  ])('saving is best effort: %s does not break the switch or leak an unhandled rejection', async (_name, answer) => {
    const unhandled = vi.fn();
    process.on('unhandledRejection', unhandled);
    const fetchMock = stubFetch(answer);
    renderWithProviders(<PersistedLanguageSwitch />, { locale: 'en' });

    fireEvent.click(screen.getByRole('link', { name: 'Español' }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
    expect(screen.getByRole('link', { name: 'Español' })).toBeInTheDocument();
  });
});
