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
import type forgeSettingsEn from '../public/locales/en/forgeSettings.json'
import type wordfeudEn from '../public/locales/en/wordfeud.json'
import type budgetEn from '../public/locales/en/budget.json'
import type salaryEn from '../public/locales/en/salary.json'
import type strideEn from '../public/locales/en/stride.json'
import type todayEn from '../public/locales/en/today.json'
import type vaultEn from '../public/locales/en/vault.json'

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
      forgeSettings: typeof forgeSettingsEn
      wordfeud: typeof wordfeudEn
      budget: typeof budgetEn
      salary: typeof salaryEn
      stride: typeof strideEn
      today: typeof todayEn
      vault: typeof vaultEn
    }
  }
}
