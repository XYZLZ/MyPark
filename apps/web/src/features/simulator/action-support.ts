'use client';

import { ApiError } from '@mypark/contracts';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { translateError, type Translator } from '@/lib/i18n/translate-error';
import { simulatorKeys } from './api';
import { isNotConfigured } from './controls';

/** What every console action reports upward, so the console can place focus (WCAG 2.4.3). */
export type ActionHooks = {
  /** After a success: the console moves focus to the status heading. */
  onDone?: () => void;
  /** After a 404 or simulator.unauthorized: the form is about to be replaced by the not configured panel. */
  onNotConfigured?: () => void;
};

/** Refetches status and events at once, so the result of an action shows before the next 2 s tick. */
export function useRefreshConsole(): () => Promise<void> {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: simulatorKeys.all });
}

type RootErrorForm = { setError: (name: 'root.serverError', error: { type: string; message: string }) => void };

/** A `useApiForm` `onError` for a missing configuration: say so on the form and let the status refetch swap the panel in. */
export function useNotConfiguredHandler(onNotConfigured?: () => void) {
  const translate = useTranslations();
  const refresh = useRefreshConsole();
  return (error: unknown, _values: unknown, form: RootErrorForm): boolean => {
    if (!isNotConfigured(error)) return false;
    form.setError('root.serverError', { type: 'server', message: translate('simulator.notConfigured') });
    onNotConfigured?.();
    void refresh();
    return true;
  };
}

/** The banner text for a failed action, never the raw code or `ApiError.message`. */
export function failureMessage(t: Translator, failure: unknown): string {
  if (isNotConfigured(failure)) return t('simulator.notConfigured');
  return translateError(t, failure instanceof ApiError ? failure : new ApiError({ code: 'internal.unexpected', message: 'unknown' }));
}
