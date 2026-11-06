import { createInstance, type i18n as I18n } from 'i18next';
import { initReactI18next } from 'react-i18next';

import en from './locales/en.json';
import esDO from './locales/es-DO.json';
import { DEFAULT_LANGUAGE, type AppLanguage } from './pickLanguage';

export { DEFAULT_LANGUAGE, SUPPORTED_LANGUAGES, pickLanguage, type AppLanguage } from './pickLanguage';

export const resources = {
  'es-DO': { translation: esDO },
  en: { translation: en },
} as const;

/** Creates a ready-to-use i18next instance. Resources are bundled, so init is synchronous. */
export function initI18n(lang: AppLanguage): I18n {
  const instance = createInstance();
  void instance.use(initReactI18next).init({
    resources,
    lng: lang,
    fallbackLng: DEFAULT_LANGUAGE,
    supportedLngs: Object.keys(resources),
    initAsync: false,
    interpolation: { escapeValue: false },
    returnNull: false,
  });
  return instance;
}
