import { ApiError } from '@mypark/contracts';
import { act, renderHook } from '@testing-library/react-native';
import { useForm, type UseFormReturn } from 'react-hook-form';
import { AccessibilityInfo } from 'react-native';

import { initI18n } from '@/lib/i18n';

import { applyApiError, retryAfterSeconds } from './apply-api-error';

const i18n = initI18n('en');

type Values = { email: string; password: string; code: string; plate: string };

async function setup() {
  const { result } = await renderHook(() => useForm<Values>({ defaultValues: { email: '', password: '', code: '', plate: '' } }));
  return result.current;
}

function apiError(code: string, details: Record<string, unknown> = {}, status = 422) {
  return new ApiError({ code, message: 'ignored', details, status });
}

function message(form: UseFormReturn<Values>, name: string) {
  return form.getFieldState(name as keyof Values).error?.message;
}

beforeEach(() => {
  jest.spyOn(AccessibilityInfo, 'announceForAccessibility').mockImplementation(() => undefined);
});
afterEach(() => jest.restoreAllMocks());

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
  ])('maps the server rule %s to its field and message', async (rule, param, expected) => {
    const form = await setup();
    const field = { field: 'email', rule, ...(param ? { param } : {}) };

    await act(async () => applyApiError(form, apiError('validation.failed', { fields: [field] }), i18n));

    expect(message(form, 'email')).toBe(expected);
    expect(form.getFieldState('email').error?.type).toBe('server');
  });

  test('focuses only the first invalid field', async () => {
    const form = await setup();
    const setError = jest.spyOn(form, 'setError');

    await act(async () =>
      applyApiError(
        form,
        apiError('validation.failed', {
          fields: [
            { field: 'email', rule: 'email' },
            { field: 'password', rule: 'min', param: '10' },
          ],
        }),
        i18n,
      ),
    );

    expect(setError.mock.calls.map((call) => call[2])).toEqual([{ shouldFocus: true }, { shouldFocus: false }]);
  });

  test('a field the form lacks falls back to the banner', async () => {
    const form = await setup();

    await act(async () => applyApiError(form, apiError('validation.failed', { fields: [{ field: 'nickname', rule: 'required' }] }), i18n));

    expect(message(form, 'root.serverError')).toBe('Check the highlighted fields.');
  });

  test.each([
    ['vehicle.plate_taken', 'plate'],
    ['auth.password_incorrect', 'password'],
    ['auth.code_invalid', 'code'],
  ])('%s lands on %s', async (code, field) => {
    const form = await setup();

    await act(async () => applyApiError(form, apiError(code, {}, 409), i18n));

    expect(message(form, field)).toBe(`errors:${code}`);
  });

  test('a mapped code the form lacks falls back to the banner', async () => {
    const { result } = await renderHook(() => useForm<{ email: string }>({ defaultValues: { email: '' } }));

    await act(async () => applyApiError(result.current, apiError('auth.code_invalid'), i18n));

    expect(result.current.getFieldState('root.serverError' as 'email').error?.message).toBe('The code is not valid or has expired.');
  });

  test('banner errors are announced and a plain Error uses the generic text', async () => {
    const form = await setup();

    await act(async () => applyApiError(form, new Error('boom'), i18n));

    const text = 'Something went wrong on our side. Please try again.';
    expect(message(form, 'root.serverError')).toBe(text);
    expect(AccessibilityInfo.announceForAccessibility).toHaveBeenCalledWith(text);
  });

  test('a 429 shows the wait time, falling back to 60 s', async () => {
    const form = await setup();

    await act(async () => applyApiError(form, apiError('rate_limit.exceeded', {}, 429), i18n));

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
