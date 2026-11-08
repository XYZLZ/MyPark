import { z } from 'zod';
import type { components } from '../openapi';
import { codeField, currentPasswordField, emailField, firstNameField, lastNameField, newPasswordField, phoneField } from './fields';
import type { LegalCurrent } from './legal';
import type { Locale } from './user';
import { userSchema } from './user';

export const tokenResponseSchema = z.strictObject({
  access_token: z.string(),
  token_type: z.literal('Bearer'),
  expires_in: z.number(),
  refresh_token: z.string(),
  refresh_expires_in: z.number(),
  legal_acceptance_required: z.boolean(),
  user: userSchema,
});

/** What `/api/auth/login` returns to the browser: no tokens. */
export const loginResultSchema = z.strictObject({
  user: userSchema,
  legal_acceptance_required: z.boolean(),
});

/** Body of 202 from verify/resend and password/forgot. */
export const acceptedStatusSchema = z.strictObject({ status: z.literal('accepted') });
/** Body of 202 from register. */
export const registerAcceptedSchema = z.strictObject({ status: z.literal('verification_pending') });

export const loginSchema = z.object({ email: emailField, password: currentPasswordField });

export const registerSchema = z.object({
  email: emailField,
  password: newPasswordField,
  first_name: firstNameField,
  last_name: lastNameField,
  phone: phoneField,
  accept_legal: z.literal(true, { error: 'required' }),
});

export const verifySchema = z.object({ email: emailField, code: codeField });
/** Forgot password and resend verification. */
export const emailOnlySchema = z.object({ email: emailField });
export const resetSchema = z.object({ email: emailField, code: codeField, new_password: newPasswordField });
export const acceptInvitationSchema = z.object({ email: emailField, code: codeField, password: newPasswordField });

export type TokenResponse = z.output<typeof tokenResponseSchema>;
export type LoginResult = z.output<typeof loginResultSchema>;
export type LoginValues = z.output<typeof loginSchema>;
export type RegisterValues = z.output<typeof registerSchema>;
export type VerifyValues = z.output<typeof verifySchema>;
export type EmailOnlyValues = z.output<typeof emailOnlySchema>;
export type ResetValues = z.output<typeof resetSchema>;
export type AcceptInvitationValues = z.output<typeof acceptInvitationSchema>;

type Schemas = components['schemas'];

export function toLoginRequest(form: LoginValues, clientKind: 'web' | 'mobile'): Schemas['LoginRequest'] {
  return { email: form.email, password: form.password, client_kind: clientKind };
}

/** Versions come from `GET /v1/legal/current`; the checkbox exists only in the form. */
export function toRegisterRequest(form: RegisterValues, legal: LegalCurrent, locale: Locale): Schemas['RegisterRequest'] {
  const { accept_legal: _accepted, phone, ...rest } = form;
  return {
    ...rest,
    ...(phone ? { phone } : {}),
    locale,
    accepted_terms_version: legal.terms.version,
    accepted_privacy_version: legal.privacy.version,
  };
}
