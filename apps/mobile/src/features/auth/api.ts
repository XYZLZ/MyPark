import {
  acceptedStatusSchema,
  legalCurrentSchema,
  parseEnvelope,
  parseNoContent,
  registerAcceptedSchema,
  tokenResponseSchema,
  toLoginRequest,
  type components,
  type LegalCurrent,
  type LoginValues,
  type ResetValues,
  type TokenResponse,
  type VerifyValues,
} from '@mypark/contracts';

import { ARGON2_TIMEOUT_MS, unwrap, type HttpRequest } from '@/services/httpClient';
import { http } from '@/services/runtime';

type RegisterRequest = components['schemas']['RegisterRequest'];

const parseTokens = parseEnvelope(tokenResponseSchema);
const parseLegal = parseEnvelope(legalCurrentSchema);
const parseRegistered = parseEnvelope(registerAcceptedSchema);
const parseAccepted = parseEnvelope(acceptedStatusSchema);
/** For 204 responses. `unwrap` hands the parser an optional status, `parseNoContent` wants one. */
const parseNothing = (body: unknown, status?: number) => parseNoContent(body, status ?? 0);

/** Public calls: no bearer, no refresh flow. Failures reject with an ApiError that keeps Retry-After. */
async function call<T>(req: Omit<HttpRequest, 'auth'>, parse: (body: unknown, status?: number) => T): Promise<T> {
  return unwrap(await http.request(req), parse);
}

export function login(form: LoginValues): Promise<TokenResponse> {
  return call({ method: 'POST', path: '/v1/auth/login', body: toLoginRequest(form, 'mobile'), timeoutMs: ARGON2_TIMEOUT_MS }, parseTokens);
}

export function fetchLegal(): Promise<LegalCurrent> {
  return call({ method: 'GET', path: '/v1/legal/current' }, parseLegal);
}

export async function register(body: RegisterRequest): Promise<void> {
  await call({ method: 'POST', path: '/v1/auth/register', body, timeoutMs: ARGON2_TIMEOUT_MS }, parseRegistered);
}

export function verify(form: VerifyValues): Promise<void> {
  return call({ method: 'POST', path: '/v1/auth/verify', body: form }, parseNothing);
}

export async function resendVerification(email: string): Promise<void> {
  await call({ method: 'POST', path: '/v1/auth/verify/resend', body: { email } }, parseAccepted);
}

export async function forgotPassword(email: string): Promise<void> {
  await call({ method: 'POST', path: '/v1/auth/password/forgot', body: { email } }, parseAccepted);
}

export function resetPassword(form: ResetValues): Promise<void> {
  return call({ method: 'POST', path: '/v1/auth/password/reset', body: form, timeoutMs: ARGON2_TIMEOUT_MS }, parseNothing);
}
