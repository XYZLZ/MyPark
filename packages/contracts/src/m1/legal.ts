import { z } from 'zod';

const legalDocumentSchema = z.strictObject({
  version: z.string(),
  published_at: z.string(),
  url_es: z.string(),
  url_en: z.string(),
});

export const legalCurrentSchema = z.strictObject({
  terms: legalDocumentSchema,
  privacy: legalDocumentSchema,
});

export const acceptLegalSchema = z.object({
  terms_version: z.string({ error: 'required' }).min(1, { error: 'required' }),
  privacy_version: z.string({ error: 'required' }).min(1, { error: 'required' }),
});

export type LegalCurrent = z.output<typeof legalCurrentSchema>;
export type AcceptLegalValues = z.output<typeof acceptLegalSchema>;
