import {
  parseEnvelope,
  parseNoContent,
  userSchema,
  type AcceptLegalValues,
  type DeleteMeValues,
  type UpdateMeValues,
  type User,
} from '@mypark/contracts';

import { ARGON2_TIMEOUT_MS, unwrap, type HttpRequest } from '@/services/httpClient';
import { http } from '@/services/runtime';

export const ME_QUERY_KEY = ['me'] as const;

const parseUser = parseEnvelope(userSchema);
/** For 204 responses. `unwrap` hands the parser an optional status, `parseNoContent` wants one. */
const parseNothing = (body: unknown, status?: number) => parseNoContent(body, status ?? 0);

/** Signed-in calls: the client sends the bearer and refreshes on a 401. Failures reject with an ApiError. */
async function call<T>(req: Omit<HttpRequest, 'auth'>, parse: (body: unknown, status?: number) => T): Promise<T> {
  return unwrap(await http.request({ ...req, auth: true }), parse);
}

export function getMe(): Promise<User> {
  return call({ method: 'GET', path: '/v1/me' }, parseUser);
}

/** PATCH is partial: only the changed fields go out. */
export function updateMe(input: Partial<UpdateMeValues>): Promise<User> {
  return call({ method: 'PATCH', path: '/v1/me', body: input }, parseUser);
}

/** Argon2 verifies the password, so this call gets the long timeout (CC-16). */
export function deleteMe(form: DeleteMeValues): Promise<void> {
  return call({ method: 'DELETE', path: '/v1/me', body: form, timeoutMs: ARGON2_TIMEOUT_MS }, parseNothing);
}

export function acceptLegal(body: AcceptLegalValues): Promise<void> {
  return call({ method: 'POST', path: '/v1/me/legal-acceptance', body }, parseNothing);
}
