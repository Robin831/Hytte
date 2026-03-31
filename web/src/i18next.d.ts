import type commonEn from '../public/locales/en/common.json'
import type dashboardEn from '../public/locales/en/dashboard.json'
import type weatherEn from '../public/locales/en/weather.json'
import type chatEn from '../public/locales/en/chat.json'
import type infraEn from '../public/locales/en/infra.json'
import type kioskEn from '../public/locales/en/kiosk.json'
import type lactateEn from '../public/locales/en/lactate.json'
import type notesEn from '../public/locales/en/notes.json'
import type settingsEn from '../public/locales/en/settings.json'
import type trainingEn from '../public/locales/en/training.json'
import type transitEn from '../public/locales/en/transit.json'
import type allowanceEn from '../public/locales/en/allowance.json'
import type workhoursEn from '../public/locales/en/workhours.json'
import type forgeEn from '../public/locales/en/forge.json'

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: {
      common: typeof commonEn
      dashboard: typeof dashboardEn
      weather: typeof weatherEn
      chat: typeof chatEn
      infra: typeof infraEn
      kiosk: typeof kioskEn
      lactate: typeof lactateEn
      notes: typeof notesEn
      settings: typeof settingsEn
      training: typeof trainingEn
      transit: typeof transitEn
      allowance: typeof allowanceEn
      workhours: typeof workhoursEn
      forge: typeof forgeEn
    }
  }
}
