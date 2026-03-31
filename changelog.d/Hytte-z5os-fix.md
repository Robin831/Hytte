category: Fixed
- **Kiosk: remove i18n dependency from kiosk components** - Kiosk display components (KioskBusDepartures, KioskWeather, KioskSunrise) used useTranslation which fails on old browsers where i18next can't initialize. Replaced with hardcoded Norwegian strings since the kiosk is a fixed wall display, not a multi-language app. (Hytte-z5os)
