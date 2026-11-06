export const SUPPORTED_LANGUAGES = ['es-DO', 'en'] as const;
export type AppLanguage = (typeof SUPPORTED_LANGUAGES)[number];
export const DEFAULT_LANGUAGE: AppLanguage = 'es-DO';

/**
 * Picks the app language from device language tags, in the device's order of preference.
 * The first tag whose language is `es` gives es-DO and `en` gives en. Anything else
 * (fr-FR, an empty list) falls back to es-DO.
 */
export function pickLanguage(deviceTags: string[]): AppLanguage {
  for (const tag of deviceTags) {
    const language = tag.split(/[-_]/)[0]?.toLowerCase();
    if (language === 'es') return 'es-DO';
    if (language === 'en') return 'en';
  }
  return DEFAULT_LANGUAGE;
}
