import { describe, expect, test } from 'vitest';
import { legalCurrentFixture, userFixture } from '../fixtures/m1';
import { acceptedStatusSchema, emailOnlySchema, registerAcceptedSchema, verifySchema } from './auth';
import { loginSchema, registerSchema, toLoginRequest, toRegisterRequest } from './auth';
import { acceptLegalSchema } from './legal';
import { userSchema } from './user';
import { vehicleInputSchema } from './vehicle';

const validRegister = {
  email: 'ana@example.do',
  password: 'correct horse 1',
  first_name: 'Ana',
  last_name: 'Perez',
  phone: '',
  accept_legal: true as const,
};

function messages(result: { error?: { issues: { path: PropertyKey[]; message: string }[] } }) {
  return result.error?.issues.map((i) => [String(i.path[0]), i.message]);
}

describe('registerSchema', () => {
  test('registerSchema accepts the +1 pre-fill as no phone', () => {
    const result = registerSchema.safeParse({ ...validRegister, phone: '+1' });
    expect(result.success).toBe(true);
    expect(result.data?.phone).toBe('');
  });

  test('registerSchema rejects a local-format phone with rule phone', () => {
    const result = registerSchema.safeParse({ ...validRegister, phone: '(809) 555-1234' });
    expect(messages(result)).toEqual([['phone', 'phone']]);
  });

  test('password needs 10 characters on register but not on login', () => {
    expect(messages(registerSchema.safeParse({ ...validRegister, password: 'short' }))).toEqual([['password', 'min:10']]);
    expect(loginSchema.safeParse({ email: 'a@b.do', password: 'short' }).success).toBe(true);
  });

  test('password longer than 128 fails with max:128', () => {
    const long = 'a'.repeat(129);
    expect(messages(registerSchema.safeParse({ ...validRegister, password: long }))).toEqual([['password', 'max:128']]);
    expect(messages(loginSchema.safeParse({ email: 'a@b.do', password: long }))).toEqual([['password', 'max:128']]);
  });

  test('registerSchema requires the legal checkbox', () => {
    expect(messages(registerSchema.safeParse({ ...validRegister, accept_legal: false }))).toEqual([['accept_legal', 'required']]);
  });

  test('empty required fields report required', () => {
    expect(messages(loginSchema.safeParse({ email: '', password: '' }))).toEqual([
      ['email', 'required'],
      ['password', 'required'],
    ]);
  });
});

describe('field rules', () => {
  test('code must be six digits', () => {
    for (const code of ['12345', '12a456', '1234567']) {
      expect(messages(verifySchema.safeParse({ email: 'a@b.do', code }))).toEqual([['code', 'code6']]);
    }
    expect(verifySchema.safeParse({ email: 'a@b.do', code: '123456' }).success).toBe(true);
  });

  test('plate keeps what the user typed', () => {
    const result = vehicleInputSchema.safeParse({ plate: 'a-1 23', make: 'Kia', model: 'Rio', color: 'Red' });
    expect(result.data?.plate).toBe('a-1 23');
  });

  test('plate length is checked on the typed value', () => {
    const base = { make: 'Kia', model: 'Rio', color: 'Red' };
    expect(messages(vehicleInputSchema.safeParse({ ...base, plate: '' }))).toEqual([['plate', 'required']]);
    expect(messages(vehicleInputSchema.safeParse({ ...base, plate: 'A' }))).toEqual([['plate', 'min:2']]);
    expect(messages(vehicleInputSchema.safeParse({ ...base, plate: 'A'.repeat(16) }))).toEqual([['plate', 'max:15']]);
  });

  test('email is trimmed and blank is required', () => {
    expect(emailOnlySchema.parse({ email: ' a@b.do ' }).email).toBe('a@b.do');
    expect(messages(emailOnlySchema.safeParse({ email: '' }))).toEqual([['email', 'required']]);
    expect(messages(emailOnlySchema.safeParse({ email: '   ' }))).toEqual([['email', 'required']]);
    expect(messages(emailOnlySchema.safeParse({ email: 'nope' }))).toEqual([['email', 'email']]);
  });

  test('legal acceptance carries both versions', () => {
    const body = { terms_version: '1', privacy_version: '2' };
    expect(acceptLegalSchema.parse(body)).toEqual(body);
  });
});

describe('builders', () => {
  test('toRegisterRequest fills versions and locale and drops the checkbox', () => {
    const request = toRegisterRequest(registerSchema.parse(validRegister), legalCurrentFixture, 'en');
    expect(request.accepted_terms_version).toBe('2026-11-16');
    expect(request.accepted_privacy_version).toBe('2026-11-16');
    expect(request.locale).toBe('en');
    expect('accept_legal' in request).toBe(false);
    expect('phone' in request).toBe(false);
  });

  test('toRegisterRequest sends a normalized phone', () => {
    const form = registerSchema.parse({ ...validRegister, phone: '+1 809-555-1234' });
    expect(toRegisterRequest(form, legalCurrentFixture, 'es-DO').phone).toBe('+18095551234');
  });

  test('toLoginRequest sets client_kind', () => {
    const form = loginSchema.parse({ email: 'a@b.do', password: 'x' });
    expect(toLoginRequest(form, 'web').client_kind).toBe('web');
    expect(toLoginRequest(form, 'mobile').client_kind).toBe('mobile');
  });
});

describe('response schemas', () => {
  test('userSchema rejects an unknown property', () => {
    expect(userSchema.safeParse(userFixture).success).toBe(true);
    expect(userSchema.safeParse({ ...userFixture, extra: 1 }).success).toBe(false);
  });

  test('accepted bodies are distinct per endpoint', () => {
    expect(acceptedStatusSchema.safeParse({ status: 'accepted' }).success).toBe(true);
    expect(acceptedStatusSchema.safeParse({ status: 'verification_pending' }).success).toBe(false);
    expect(registerAcceptedSchema.safeParse({ status: 'verification_pending' }).success).toBe(true);
    expect(registerAcceptedSchema.safeParse({ status: 'accepted' }).success).toBe(false);
  });
});
