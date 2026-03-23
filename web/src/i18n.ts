import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import HttpBackend from 'i18next-http-backend'

i18n
  .use(HttpBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs: ['en', 'nb', 'th'],
    defaultNS: 'common',
    interpolation: {
      escapeValue: false,
    },
    backend: {
      loadPath: '/locales/{{lng}}/{{ns}}.json',
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'hytte-language',
    },
  })

i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})

// Set initial lang attribute once i18n resolves
if (i18n.isInitialized) {
  document.documentElement.lang = i18n.language
} else {
  i18n.on('initialized', () => {
    document.documentElement.lang = i18n.language
  })
}

export default i18n
