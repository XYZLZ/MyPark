import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

// The real navigation helpers need the Next.js router; see testing/navigation.tsx.
vi.mock('@/i18n/navigation', async () => await import('./navigation'));
