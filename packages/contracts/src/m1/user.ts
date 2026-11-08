import { z } from 'zod';
import { firstNameField, lastNameField, phoneField, currentPasswordField } from './fields';

const localeSchema = z.enum(['es-DO', 'en']);
const statusSchema = z.enum(['pending_verification', 'active', 'suspended']);

export type Locale = z.infer<typeof localeSchema>;

export const userSchema = z.strictObject({
  id: z.string(),
  email: z.string(),
  first_name: z.string(),
  last_name: z.string(),
  phone: z.string().nullable(),
  role: z.enum(['driver', 'admin']),
  status: statusSchema,
  locale: localeSchema,
  email_verified_at: z.string().nullable(),
  legal: z.strictObject({
    terms_version: z.string().nullable(),
    terms_accepted_at: z.string().nullable(),
    privacy_version: z.string().nullable(),
    privacy_accepted_at: z.string().nullable(),
  }),
  created_at: z.string(),
});

export const adminUserSchema = z.strictObject({
  id: z.string(),
  email: z.string(),
  first_name: z.string(),
  last_name: z.string(),
  status: statusSchema,
  locale: localeSchema,
  created_at: z.string(),
});

export const updateMeSchema = z.object({
  first_name: firstNameField,
  last_name: lastNameField,
  phone: phoneField,
});

export const deleteMeSchema = z.object({ password: currentPasswordField });

export type User = z.output<typeof userSchema>;
export type UpdateMeValues = z.output<typeof updateMeSchema>;
export type DeleteMeValues = z.output<typeof deleteMeSchema>;
