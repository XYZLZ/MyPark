import { ApiError } from '@mypark/contracts';
import { act, renderHook } from '@testing-library/react';
import { createTranslator } from 'next-intl';
import { useForm, type UseFormReturn } from 'react-hook-form';
import { describe, expect, test, vi } from 'vitest';
import en from '@/i18n/messages/en.json';
import type { Translator } from '@/lib/i18n/translate-error';
import { applyApiError, retryAfterSeconds } from './apply-api-error';

const t = createTranslator({ locale: 'en', messages: en }) as unknown as Translator;

type Values = { email: string; password: string; code: string; plate: string };

function setup() {
  const { result } = renderHook(() => useForm<Values>({ defaultValues: { email: '', password: '', code: '', plate: '' } }));
  return result.current;
}

function apiError(code: string, details: Record<string, unknown> = {}, status = 422) {
  return new ApiError({ code, message: 'ignored', details, status });
}

function message(form: UseFormReturn<Values>, name: string) {
  return form.getFieldState(name as keyof Values).error?.message;
}

describe('applyApiError', () => {
  test.each([
    ['required', undefined, 'required'],
    ['email', undefined, 'email'],
    ['min', '10', 'min:10'],
    ['max', '128', 'max:128'],
    ['oneof', undefined, 'oneof'],
    ['phone', undefined, 'phone'],
    ['code6', undefined, 'code6'],
    ['password_common', undefined, 'password_common'],
    ['plate', undefined, 'plate'],
  ])('maps the server rule %s to its field and message', (rule, param, expected) => {
    const form = setup();
    const field = { field: 'email', rule, ...(param ? { param } : {}) };

    act(() => applyApiError(form, apiError('validation.failed', { fields: [field] }), t));

    expect(message(form, 'email')).toBe(expected);
    expect(form.getFieldState('email').error?.type).toBe('server');
  });

  test('shouldFocus is true only on the first field', () => {
    const form = setup();
    const setError = vi.spyOn(form, 'setError');

    act(() =>
      applyApiError(
        form,
        apiError('validation.failed', {
          fields: [
            { field: 'email', rule: 'email' },
            { field: 'password', rule: 'min', param: '10' },
          ],
        }),
        t,
      ),
    );

    expect(setError.mock.calls.map((call) => call[2])).toEqual([{ shouldFocus: true }, { shouldFocus: false }]);
  });

  test('a field the form lacks falls back to the banner', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('validation.failed', { fields: [{ field: 'nickname', rule: 'required' }] }), t));

    expect(message(form, 'root.serverError')).toBe('Check the highlighted fields.');
  });

  test('vehicle.plate_taken lands on plate', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('vehicle.plate_taken', {}, 409), t));

    expect(message(form, 'plate')).toBe('errors:vehicle.plate_taken');
  });

  test('auth.password_incorrect lands on password', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('auth.password_incorrect', {}, 403), t));

    expect(message(form, 'password')).toBe('errors:auth.password_incorrect');
  });

  test('auth.code_invalid lands on code', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('auth.code_invalid'), t));

    expect(message(form, 'code')).toBe('errors:auth.code_invalid');
  });

  test('a mapped code the form lacks falls back to the banner', () => {
    const { result } = renderHook(() => useForm<{ email: string }>({ defaultValues: { email: '' } }));
    const form = result.current;

    act(() => applyApiError(form, apiError('auth.code_invalid'), t));

    expect(message(form as unknown as UseFormReturn<Values>, 'root.serverError')).toBe('The code is not valid or has expired.');
  });

  test('other codes go to root.serverError, translated', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('auth.invalid_credentials', {}, 401), t));

    expect(message(form, 'root.serverError')).toBe('The email or password is not correct.');
  });

  test('a plain Error uses the generic text', () => {
    const form = setup();

    act(() => applyApiError(form, new Error('boom'), t));

    expect(message(form, 'root.serverError')).toBe('Something went wrong on our side. Please try again.');
  });

  test('a 429 shows the wait time, falling back to 60 s', () => {
    const form = setup();

    act(() => applyApiError(form, apiError('rate_limit.exceeded', {}, 429), t));

    expect(message(form, 'root.serverError')).toBe('Too many attempts. Try again in 60 s.');
  });
});

describe('retryAfterSeconds', () => {
  test('prefers details, then the header', () => {
    expect(retryAfterSeconds(apiError('rate_limit.exceeded', { retry_after_seconds: 42, retry_after_header: '9' }, 429))).toBe(42);
    expect(retryAfterSeconds(apiError('rate_limit.exceeded', { retry_after_header: '9' }, 429))).toBe(9);
  });

  test.each([
    ['both absent', {}],
    ['Retry-After soon', { retry_after_header: 'soon' }],
    ['zero in details', { retry_after_seconds: 0 }],
    ['zero in the header', { retry_after_header: '0' }],
    ['a negative value', { retry_after_seconds: -5 }],
    ['a fraction', { retry_after_header: '1.5' }],
    ['a date', { retry_after_header: 'Wed, 21 Oct 2026 07:28:00 GMT' }],
  ])('retryAfterSeconds falls back to 60: %s', (_name, details) => {
    expect(retryAfterSeconds(apiError('rate_limit.exceeded', details, 429))).toBe(60);
  });
});
