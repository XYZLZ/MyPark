import { tokenResponseSchema } from '../m1/auth';
import { legalCurrentSchema } from '../m1/legal';
import { userSchema } from '../m1/user';
import { vehicleSchema } from '../m1/vehicle';
import type { FieldError } from '../m1/validation';

/** Typed fixtures for client tests. Each is parsed by its schema at module load, so it cannot drift. */

const legalAccepted = {
  terms_version: '2026-11-16',
  terms_accepted_at: '2026-11-16T12:00:00Z',
  privacy_version: '2026-11-16',
  privacy_accepted_at: '2026-11-16T12:00:00Z',
};

export const userFixture = userSchema.parse({
  id: '0b6f6f4e-8f2e-4c8e-9d63-5f1f0a1b2c3d',
  email: 'ana@example.do',
  first_name: 'Ana',
  last_name: 'Perez',
  phone: '+18095551234',
  role: 'driver',
  status: 'active',
  locale: 'es-DO',
  email_verified_at: '2026-11-16T11:59:00Z',
  legal: legalAccepted,
  created_at: '2026-11-16T11:58:00Z',
});

/** An admin account, as returned by `GET /v1/me`. */
export const adminFixture = userSchema.parse({
  ...userFixture,
  id: '5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f',
  email: 'admin@example.do',
  first_name: 'Admin',
  last_name: 'MyPark',
  phone: null,
  role: 'admin',
});

export const tokenResponseFixture = tokenResponseSchema.parse({
  access_token: 'access-1',
  token_type: 'Bearer',
  expires_in: 900,
  refresh_token: 'refresh-1',
  refresh_expires_in: 2592000,
  legal_acceptance_required: false,
  user: userFixture,
});

export const vehicleFixture = vehicleSchema.parse({
  id: '7d8e9f0a-1b2c-4d3e-8f4a-5b6c7d8e9f0a',
  plate: 'A123456',
  plate_normalized: 'A123456',
  make: 'Kia',
  model: 'Rio',
  color: 'Red',
  created_at: '2026-11-16T12:00:00Z',
  updated_at: '2026-11-16T12:00:00Z',
});

const legalDocument = {
  version: '2026-11-16',
  published_at: '2026-11-16T00:00:00Z',
  url_es: 'http://localhost:3000/es-DO/legal/terms',
  url_en: 'http://localhost:3000/en/legal/terms',
};

export const legalCurrentFixture = legalCurrentSchema.parse({
  terms: legalDocument,
  privacy: { ...legalDocument, url_es: 'http://localhost:3000/es-DO/legal/privacy', url_en: 'http://localhost:3000/en/legal/privacy' },
});

const meta = { request_id: '01HZX3V8Q2W9K7M4N5P6R7S8T9' };

export function successBody<T>(data: T) {
  return { data, meta };
}

export function listBody<T>(items: T[], limit: number) {
  return { data: items, meta: { ...meta, page: { limit, next_cursor: null } } };
}

export function errorBody(code: string, details: Record<string, unknown> = {}) {
  return { error: { code, message: code, details }, meta };
}

export function validationBody(fields: FieldError[]) {
  return errorBody('validation.failed', { fields });
}
